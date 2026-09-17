# EFG
Technical Assignment: Player Presence Service

Go project using [Box](https://github.com/fulldump/box) for the API and
[goconfig](https://github.com/fulldump/goconfig) for configuration. 
HTTP assertions and routing alternatives use [Biff](https://github.com/fulldump/biff).

## Requirements

Go 1.25.6 or later, Make, and Docker with Docker Compose.

## Development

```sh
make run                         # Start MongoDB, then the server on :8080
make run ARGS='-addr :9090'      # Use a different address
make test                        # Run all tests, except integration and load tests
make build                       # Build Linux, Windows, and macOS binaries in bin/
make test-integration            # tests with real mongodb
make test-load
  pkg: efg/presence
  cpu: Intel(R) Core(TM) i7-9700K CPU @ 3.60GHz
  BenchmarkEvents-8          21888            276221 ns/op              3620 batches/s         36203 events/s
```

The API exposes `GET /health`, which returns `{"status":"ok"}`.

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
For this demo, populate that field directly in MongoDB; there is no friend
management endpoint. Lists of up to 50 friends need no pagination.

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
  "mongo_uri": "mongodb://localhost:27017/efg"
}
```

## Help

```sh
make help
```
