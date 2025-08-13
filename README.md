# Running the tests

You will need to have [Varnish Enterprise](https://docs.varnish-software.com/varnish-enterprise/installation/) installed first.

``` bash
./run.sh
```

Or, manually

```
go get
go test
```

# Using a container

## Build a docker image with varnish+go

``` bash
docker build -t varnish-go-tests .
```

## Run the tests from the current director

``` bash
docker run -it --rm -v $(pwd):/tmp/tests --entrypoint "" -w /tmp/tests varnish-go-tests ./run.sh
```
