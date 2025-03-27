IMAGE=slicer-ssh-agent
OWNER=alexellis2
SERVER=ttl.sh
TAG=latest
LDFLAGS := "-s -w"

dist:
	GOOS=linux GOARCH=amd64 go build -ldflags $(LDFLAGS) -o bin/slicer-ssh-agent .

dist-all:
	GOOS=linux GOARCH=amd64 go build -ldflags $(LDFLAGS) -o bin/slicer-ssh-agent .
	GOOS=linux GOARCH=arm64 go build -ldflags $(LDFLAGS) -o bin/slicer-ssh-agent-arm64 .

publish:
	docker buildx build -t $(SERVER)/$(OWNER)/$(IMAGE):$(TAG) . \
		--platform linux/amd64,linux/arm64 \
		--push

