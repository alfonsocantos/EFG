# EFG
Technical Assignment: Player Presence Service

Go project using [Box](https://github.com/fulldump/box) for the API and
[goconfig](https://github.com/fulldump/goconfig) for configuration. 
HTTP assertions and routing alternatives use [Biff](https://github.com/fulldump/biff).

## Requirements

Go 1.25.6 or later, Make, and Docker with Docker Compose.

## Development

```sh
make run                         # Start the complete demo with Docker Compose
make stop                        # Stop the demo; preserve MongoDB data
make seed                        # Add missing demo players to local MongoDB
make run-api ARGS='-addr :9090'   # Run only the API locally with Go
make test                        # Unit tests for the API and web server
make build                       # Build presence API binaries in bin/
make test-integration            # MongoDB integration tests, including the seed
make test-load                   # HTTP presence event benchmark
```

## Web demo

Run `make run`, then open **http://localhost:8081**. Docker Compose starts
MongoDB, seeds eight demo players plus 100,000 synthetic players, and starts the presence API on port 8080 and
the web server on port 8081, plus a background presence simulator. The first build downloads Go and MongoDB images.

Enter a player ID, such as `demo-alex` or `demo-jamie`, and click **Show friends**.
The page displays each friend's ID, state, and game (unless offline).
The **Your presence** panel shows the selected player's visible state. Use
**Appear offline** to hide it from friends, and **Show my status** to restore
visibility. This saves `offline_mode` without changing the matchmaking state.
The list refreshes automatically every second, with no overlapping requests.
Connection errors keep the last list visible with a warning and retry automatically.

- `cmd/web/main.go` serves `cmd/web/www` at `/`, using only Go's standard library.
- `cmd/web/www/` uses locally served Vue 3.5.27 and generated Tailwind CSS 4.1.18.
  Both assets and their MIT licenses are included under `vendor/`. No CDN, Node,
  or frontend build is needed to run the demo.
- The web server proxies `/api/*` to the presence API so the browser can use
  the existing `X-Player-ID` header without cross-origin configuration.
- `cmd/seed/` preserves the eight `demo-*` players and their seven friends, then
  adds 100,000 `load-*` players with random initial states, games and ten friends.
  Inserts use batches of 1,000. Stable IDs and insert-only upserts make reruns
  safe, preserving existing states, friends and preferences.
- The Compose `simulator` uses 32 concurrent background clients, each sending
  batches of up to ten players at a minimum interval of 20 ms. Each client owns
  separate players, so concurrent clients do not generate conflicting transitions.
  Actual throughput depends on response times and backpressure.
- Every 500 ms a separate loop changes one of the eight visible demo players.
  Normal transitions are offline → online, online → ingame or offline, and
  ingame → online. There is also a 10% chance of disconnecting the chosen player.
  Games are assigned on entering ingame and cleared when leaving. Offline mode
  remains untouched, so `demo-casey` always appears offline.

`make run` seeds before starting the simulator. To run them locally with the
API already running:

```sh
make seed
make seed ARGS='-simulate -api http://localhost:8080'
```

Tune load with `-users`, `-clients` and `-interval`. Use the same `-users` value
for seeding and simulation; the simulation does not reseed the database.
For example, to reduce background traffic:

```sh
make seed ARGS='-simulate -clients 4 -interval 100ms'
```

To stop automatic changes while keeping the demo running:

```sh
docker compose stop simulator
```

To rebuild the CSS after adding Tailwind classes, download the
[Tailwind standalone CLI v4.1.18](https://github.com/tailwindlabs/tailwindcss/releases/tag/v4.1.18)
for your platform and run from the repository root:

```sh
tailwindcss -i cmd/web/tailwind.css -o cmd/web/www/vendor/tailwind.css --minify
```

Vue's production build is downloaded from
[vue@3.5.27](https://cdn.jsdelivr.net/npm/vue@3.5.27/dist/vue.global.prod.js).

Compose mounts `cmd/web/www` read-only, so frontend edits need only a browser
refresh. To run the web server locally, with the API already running, use:

```sh
go run ./cmd/web -addr :8081 -api http://localhost:8080
```

Run this command from the repository root; the default static directory is
`cmd/web/www`. `make run` uses the container's MongoDB address;
`make run-api` still reads the local `config.json`.

The web page also polls `GET /health` every second to show the queue length,
capacity and busy workers. If health cannot be fetched, it shows an unavailable
message instead of stale load values. These are snapshots: short jobs may finish
between polls, so the demo often shows zero load.

`GET /health` remains HTTP 200 even when the event queue is full:

```json
{
  "status": "ok",
  "events_per_second": 12500,
  "queue": {"length": 12, "capacity": 100},
  "workers": {"busy": 4, "total": 4}
}
```

`events_per_second` is the approximate rate of events in successfully processed
batches during the last sampling interval (about one second). It counts events,
not requests, with one atomic addition per successful batch. It includes stale
or duplicate events ignored by MongoDB, and excludes rejected/failed batches
(even if a failed bulk write partially succeeded). It is a throughput metric,
not an exact count of state changes. A separate timer samples the counter, so
multiple health consumers do not reset each other's measurements. It starts at
zero and returns to zero after a full idle sampling interval.

### Event backpressure

`POST /events` places each batch in a bounded in-memory queue. `queue_size`
(default 100) limits waiting batches; `workers` (default 4) limits simultaneous
MongoDB writes. Both must be positive. Active batches are not counted in the
queue length. Other endpoints do not use the queue.

The request waits for persistence before reporting success. When the queue is
full, the API immediately returns `503 Service Unavailable` with `Retry-After: 1`.
Clients should retry the same batch, preserving `occurred_at_ms`. The demo
simulator tries up to three times on 503, waiting one second plus jitter between
attempts; after that it logs the failure and continues the simulation. Health is an
approximate snapshot, not a reservation of capacity. Disconnected requests cancel
their work; a write may already have reached MongoDB, so retrying the same events
is safe with the existing timestamp check. Pending batches are not durable across
process restarts and must be retried by the client.

On `SIGINT` (Ctrl+C) or `SIGTERM`, the service stops accepting new requests and
event batches, then waits for active and queued jobs to finish. `shutdown_timeout`
(default `"30s"`) limits this drain period. Once it expires, remaining processing
is canceled and active HTTP connections are closed. MongoDB is disconnected
afterwards, with a separate cleanup timeout of five seconds. The Compose presence
service allows 40 seconds before forcing termination; increase `stop_grace_period`
if you raise the shutdown timeout. A second termination signal forces an exit.


```sh
curl http://localhost:8080/health
```

## My presence

The demo identifies the current player through the `X-Player-ID` header.
This is a trusted identity header for the demo, not an authentication mechanism.

```sh
curl -X PUT http://localhost:8080/me/status \
  -H 'X-Player-ID: player-42' \
  -H 'Content-Type: application/json' \
  -d '{"offline_mode":true}'

curl http://localhost:8080/me/status -H 'X-Player-ID: player-42'
# {"state":"offline","offline_mode":true}
```

`PUT /me/status` returns `204` and stores the preference, creating the player
if necessary. Send `false` to disable it. `GET /me/status` returns the visible
state: offline mode overrides the latest matchmaking state without changing
it in MongoDB. New events preserve the preference. A player without events
appears offline. Missing identity returns `401`; invalid input returns `400`.

## Friends' presence

`GET /me/friends/presence` uses `X-Player-ID` and reads the player's `friends`
array from `efg.players`, for example `{"friends":["player-7","player-8"]}`.
The demo seed populates this field;

```sh
curl http://localhost:8080/me/friends/presence -H 'X-Player-ID: player-42'
```

```json
[
  {"player_id":"player-7","state":"ingame","game":"chess"},
  {"player_id":"player-8","state":"offline"}
]
```

The endpoint reads the friend list, then fetches all their presences in one
query and returns them in list order. Unknown players and players without
events appear offline. `offline_mode` also makes a friend appear offline.
Offline friends never expose `game`; an empty friend list returns `[]`.

Matchmaking events accept a `game` string identifying the game, for example `CSGO`.
The latest accepted event updates both state and game; omitted `game` clears
the previously stored game. Non-offline friends expose `game` when supplied.

## Configuration

`config.json` sets the HTTP listen address (`addr`, default: `:8080`) and
the required MongoDB connection URI (`mongo_uri`). goconfig loads this file
automatically, if present.

Example `config.json` (loaded automatically if present):

```json
{
  "addr": ":8080",
  "mongo_uri": "mongodb://localhost:27017/efg",
  "queue_size": 100,
  "workers": 4,
  "shutdown_timeout": "30s"
}
```

## Help

```sh
make help
```
