.PHONY: build test race bench lint vet clean docker-up docker-down

GOCMD   = go
GOBUILD = $(GOCMD) build
GOTEST  = $(GOCMD) test
GOVET   = $(GOCMD) vet

build:
	$(GOBUILD) ./...

test:
	$(GOTEST) ./...

race:
	$(GOTEST) -race ./...

bench:
	cd benchmarks && $(GOTEST) -bench="." -benchmem -benchtime=3s .

lint:
	staticcheck ./...

vet:
	$(GOVET) ./...

clean:
	rm -rf bin/
	find . -name "*.test" -delete

docker-up:
	docker compose --profile full up --build -d

docker-down:
	docker compose --profile full down