# Contributing to BitTrawler

BitTrawler is a self-hosted BitTorrent indexer originally based on a fork of [bitmagnet](https://github.com/bitmagnet-io/bitmagnet). Very little of bitmagnet remains as we have chosen to pursue a completely different architecture for both the front and back-end.

## Before you start

Everyone is welcome to submit ideas and contributions. For any substantial changes, open an issue first and describe what it is you want to implement. This is an opinionated project. We're up for adding new things, but we need to make sure it makes sense and that it has been designed with performance in mind. bitmagnet ran into a lot of trouble trying to implement a plugin system, which ultimately deadlocked its development. At no point in the 100k line rewrite of the entire architecture did they stop to ask whether users actually need a plugin system. We've learned from those mistakes.

Our philosophy is: **keep it lean and obsess about the end user**. Users don't need a rust re-write, they need sonarr searches to work properly. They need to use as little disk space as possible, and avoid wearing out their SSD. Before you build anything, ask yourself why an end user would need it. If they do need it, ask yourself how they would want it to work. That's how you should build it. It should just work.

## What we want

- Bug fixes
- Better testing
- Performance improvements which are benchmarked and consider real-world use. (There's no point spending weeks optimising a search for "1080p" that will return 10m results, since nobody will ever search for that phrase in normal use. Sonarr searches and show titles are what people care about.)
- Better classification - e.g. a new general-purpose rule that will improve classification without unintended consequences
- Improved user experience
- Better documentation

## What is not wanted

- Random third-party dependencies
- Big, complicated changes like plugin systems which are unlikely to be used in the real-world. If there's something like an integration that users are likely to need, we should just build it directly into the software rather than adding complex abstractions.

## Benchmarks

Whenever you are designing a feature or planning to change how something works, think about opportunities for measurements and benchmarks. When I chose SQLite for this project, people told me I was mad. But I benchmarked it, put a bunch of work into the schema design and now it runs perfectly, and at a fraction of bitmagnet's size. Be bold, but benchmark. 

## Building

Go is the only requirement, at the version in `go.mod`:

```sh
go build .
./bittrawler version
```

## Tests

```sh
gofmt -l .        # must print nothing
go vet ./...
go test ./...
go test -race ./...
```

Further testing is conducted against the real binary with Playwright to check axe accessibility rules, ARIA snapshots and Lighthouse budgets. It needs Node and downloads its own Chromium:

```sh
bash test/browser/run.sh
```

CI runs the same tests, plus some fuzz testing on the parsers, `govulncheck`, a container test and `goreleaser check`. A pull request that fails any of them is not ready.

## Pull requests

- One change per pull request.
- Include benchmarks/measurements. We want to know the performance impact of changes you are making.
- Make sure it is thoroughly tested, including running the branch on your own server and ensuring it works end to end.
- Don't just submit AI slop. If you can't even be bothered to write your own PR message, we can't be bothered to read it.

## Licence

BitTrawler is MIT licensed, and contributions are accepted under the same licence.
