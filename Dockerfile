FROM --platform=${BUILDPLATFORM:-linux/amd64} golang:1.23 as build

ARG TARGETPLATFORM
ARG BUILDPLATFORM
ARG TARGETOS
ARG TARGETARCH

ARG VERSION
ARG GIT_COMMIT
ARG PUBLIC_KEY

ENV CGO_ENABLED=0
ENV GO111MODULE=on
ENV GOFLAGS=-mod=vendor

WORKDIR /go/src/github.com/self-actuated/slicer-ssh-agent
COPY . .

# RUN CGO_ENABLED=${CGO_ENABLED} GOOS=${TARGETOS} GOARCH=${TARGETARCH} go test -v ./...

RUN GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
        --ldflags "-s -w -X 'main.Version=${VERSION}' -X 'main.GitCommit=${GIT_COMMIT}'" \
        -o slicer-ssh-agent .

FROM --platform=${TARGETPLATFORM:-linux/amd64} scratch as ship

COPY --from=build /go/src/github.com/self-actuated/slicer-ssh-agent/slicer-ssh-agent    /

CMD ["/slicer-ssh-agent"]
