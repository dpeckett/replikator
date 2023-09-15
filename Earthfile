VERSION 0.7
FROM golang:1.21-bookworm
WORKDIR /app

docker-all:
  BUILD --platform=linux/amd64 --platform=linux/arm64 +docker

docker:
  ARG TARGETARCH
  ARG VERSION
  FROM gcr.io/distroless/static:nonroot
  WORKDIR /
  COPY LICENSE /usr/local/share/replikator/
  COPY (+replikator/replikator --GOARCH=${TARGETARCH}) /manager
  USER 65532:65532
  ENTRYPOINT ["/manager"]
  SAVE IMAGE --push ghcr.io/gpu-ninja/replikator:${VERSION}
  SAVE IMAGE --push ghcr.io/gpu-ninja/replikator:latest

bundle:
  FROM +tools
  COPY config ./config
  COPY hack ./hack
  ARG VERSION
  RUN ytt --data-value version=${VERSION} -f config -f hack/set-version.yaml | kbld -f - > replikator.yaml
  SAVE ARTIFACT ./replikator.yaml AS LOCAL dist/replikator.yaml

replikator:
  ARG GOOS=linux
  ARG GOARCH=amd64
  COPY go.mod go.sum ./
  RUN go mod download
  COPY . .
  RUN CGO_ENABLED=0 go build -ldflags '-s' -o replikator cmd/replikator/main.go
  SAVE ARTIFACT ./replikator AS LOCAL dist/replikator-${GOOS}-${GOARCH}

generate:
  FROM +tools
  COPY . .
  RUN controller-gen object:headerFile="hack/boilerplate.go.txt" paths="./..." \
    && controller-gen rbac:roleName=dex-manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases
  SAVE ARTIFACT ./config/crd/bases AS LOCAL config/crd/bases
  SAVE ARTIFACT ./config/rbac/role.yaml AS LOCAL config/rbac/role.yaml

tidy:
  LOCALLY
  RUN go mod tidy
  RUN go fmt ./...

lint:
  FROM golangci/golangci-lint:v1.54.2
  WORKDIR /app
  COPY . ./
  RUN golangci-lint run --timeout 5m ./...

test:
  COPY go.mod go.sum ./
  RUN go mod download
  COPY . .
  RUN go test -coverprofile=coverage.out -v ./...
  SAVE ARTIFACT ./coverage.out AS LOCAL coverage.out

tools:
  RUN apt update && apt install -y git ca-certificates curl libdigest-sha-perl
  RUN curl -fsSL https://carvel.dev/install.sh | bash
  ARG CONTROLLER_TOOLS_VERSION=v0.12.0
  RUN go install sigs.k8s.io/controller-tools/cmd/controller-gen@${CONTROLLER_TOOLS_VERSION}