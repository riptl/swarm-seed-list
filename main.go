package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
	"github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	"github.com/ybbus/jsonrpc"
	"net"
	"net/http"
	"os"
	"sync"
	"time"
)

var cl *client.Client

// Config
var (
	privateKey      ed25519.PrivateKey
	services        []string
	networkName     string
	refreshInterval time.Duration
	listen          string
)

func main() {
	pflag.StringSliceVar(&services, "service", []string{"validator"}, "Names of services to expose")
	pflag.StringVar(&networkName, "network", "devnet", "Name of network to use")
	pflag.DurationVar(&refreshInterval, "refresh", 1 * time.Minute, "Refresh interval")
	pflag.StringVarP(&listen, "listen", "l", ":8080", "Listen port")
	pk := os.Getenv("SEED_PRIVATE_KEY")
	if pk != "" {
		buf, err := hex.DecodeString(pk)
		if err != nil || len(buf) != ed25519.SeedSize {
			logrus.Fatal("Invalid $SEED_PRIVATE_KEY")
		}
		privateKey = ed25519.NewKeyFromSeed(buf)
	}
	pflag.Parse()

	var err error
	cl, err = client.NewEnvClient()
	if err != nil {
		logrus.WithError(err).Fatal("Failed to build Docker client")
	}
	defer func() {
		if err := cl.Close(); err != nil {
			logrus.WithError(err).Error("Failed to close Docker client")
		}
	}()

	ctx := context.Background()

	var m sync.RWMutex
	var seedList []byte
	seedList, err = generateSeedList(ctx)
	if err != nil {
		logrus.WithError(err).Fatal("Failed to generate initial seed list")
	}
	go func() {
		for range time.Tick(refreshInterval) {
			if newSeedList, err := generateSeedList(context.Background()); err != nil {
				logrus.WithError(err).Error("Failed to refresh seed list")
			} else {
				m.Lock()
				seedList = newSeedList
				m.Unlock()
			}
		}
	}()
	if err := http.ListenAndServe(listen, http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		m.RLock()
		defer m.RUnlock()
		res.Header().Set("Content-Type", "text/plain; charset=UTF-8")
		res.WriteHeader(http.StatusOK)
		_, _ = res.Write(seedList)
	})); err != nil {
		logrus.WithError(err).Error("HTTP server died")
	}
}

func generateSeedList(ctx context.Context) ([]byte, error) {
	urls, err := getURLs(ctx)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	for _, u := range urls {
		buf.WriteString(u)
		buf.WriteByte('\n')
	}
	if privateKey != nil {
		sig := ed25519.Sign(privateKey, buf.Bytes())
		buf.WriteByte('\n')
		buf.WriteString(hex.EncodeToString(sig))
		buf.WriteByte('\n')
	}
	return buf.Bytes(), nil
}

func getURLs(ctx context.Context) (urls []string, err error) {
	// Get list of tasks
	args := filters.NewArgs()
	args.Add("desired-state", "running")
	for _, service := range services {
		args.Add("service", service)
	}
	list, err := cl.TaskList(ctx, types.TaskListOptions{Filters: args})
	if err != nil {
		return nil, err
	}

	// Start spawning goroutines to request
	// public keys required to build URLs
	var wg sync.WaitGroup
	urlChan := make(chan string)

	for _, task := range list {
		// Ignore non-running tasks
		if task.DesiredState != swarm.TaskStateRunning ||
			task.Status.State != swarm.TaskStateRunning {
			continue
		}
		// Search networks to check if attached to target network,
		// ignore otherwise.
		var address string
		for _, attachment := range task.NetworksAttachments {
			if attachment.Network.Spec.Name == networkName &&
				len(attachment.Addresses) > 0 {
				address = attachment.Addresses[0]
				break
			}
		}
		if address == "" {
			continue
		}
		// Parse network CIDR
		ip, _, err := net.ParseCIDR("address")
		if err != nil {
			logrus.WithError(err).WithField("task", task.ID).
				Error("Failed to parse CIDR")
			continue
		}
		ipStr := ip.String()
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Request public key
			rpc := jsonrpc.NewClientWithOpts(fmt.Sprintf("http://%s:8648", ipStr), &jsonrpc.RPCClientOpts{
				HTTPClient: &http.Client{
					Timeout: 3 * time.Second,
				},
			})
			var publicKey string
			if err := rpc.CallFor(&publicKey, "peer_public_key"); err != nil {
				logrus.WithError(err).WithField("task", task.ID).
					Error("Failed to request peer public key")
			} else {
				// Having pubkey, all information to build seed URL is available
				urlChan <- fmt.Sprintf("ws://%s:8443/%s", ipStr, publicKey)
			}
		}()
	}
	go func() {
		wg.Wait()
		close(urlChan)
	}()
	for u := range urlChan {
		urls = append(urls, u)
	}
	return
}
