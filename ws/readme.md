


## Testing

Using this [advice](https://dev.to/wallyqs/introducing-gotest-mod-18a1) to keep
library dependencies tiny. Integration tests has lot of dependencies because
they are starting docker container for autobahn test.

To run unit test of the ws package:
```
go test -v github.com/ianic/ws
```

 To run integration tests from the test package:
```
go test ./test -modfile=go_test.mod -v
```
or
```
go test ./... -modfile=go_test.mod -v
```

To recreate go_test.mod from go.mod:
```
rm go_test.* && cp go.mod go_test.mod && \
go get -modfile=go_test.mod github.com/klauspost/compress && \
go get -modfile=go_test.mod github.com/ory/dockertest/v3 && \
go get -modfile=go_test.mod golang.org/x/sync/errgroup && \
```
