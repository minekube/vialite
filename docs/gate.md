# Gate Integration

`vialite` is designed for Gate classic backend connections.

Expected Gate topology:

```text
player / Bedrock
  -> optional geyserlite
  -> Gate classic
  -> vialite
  -> backend
```

Gate should start `vialite` with a backend list, wait for readiness, then
dial `BackendDialAddress(serverName)` for servers configured to use Via
translation.

Example config shape:

```yaml
via:
  enabled: true
  mode: embedded
  gate_protocol: auto
  backends:
    - name: lobby
      address: 127.0.0.1:25566
      version: auto
      forwarding: velocity
```

The adapter package lives at:

```text
go.minekube.com/vialite/integration/gate
```

It exposes a fakeable wrapper so Gate tests do not require a real native
library.

## Not Lite

Gate Lite reads the initial handshake, chooses a backend, writes the
handshake, and then raw-pipes bytes. That is exactly why it preserves
backend-owned auth. Via translation must decode and rewrite packets after
login, so it belongs behind Gate classic, not in Lite.

## First Integration Point

The first Gate integration should use the existing backend dial hook:

```go
type ServerDialer interface {
    Dial(ctx context.Context, player Player) (net.Conn, error)
}
```

A translated server implementation can dial `vialite.BackendDialAddress`
instead of the raw backend address while keeping Gate's existing backend
login flow.

`Start` runs until shutdown. Gate integration code should launch it on the
same lifecycle as other long-running services, call `WaitReady`, and only
then route players to Via-backed server entries.
