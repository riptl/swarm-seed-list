# Swarm Seed List Generator

This service is intended to improve connectivity in a Nimiq network running on Docker Swarm.
It spawns a web server that serves a seed list with links to other peers.
Requirements:
 - Access to Docker Daemon API (`/var/run/docker.sock` mounted)
 - Attached to all specified `network`s
 - Containers in service have open RPC servers at port `8648`

```
Usage of ./swarm-seed-list:
  -l, --listen string      Listen port (default ":8080")
      --network string     Name of network to use (default "devnet")
      --refresh duration   Refresh interval (default 1m0s)
      --service strings    Names of services to expose (default [validator])

  $LIST_PRIVATE_KEY        Hex-encoded Ed25519 private key for signing the list (optional)
```

### Generating the seed list

It looks up all peers in specified `service`s that are present in `network`,
then it calls `peer_public_key` on each of them to build a list of seed URLs.

This seed list gets rebuilt in intervals of `refresh`.
