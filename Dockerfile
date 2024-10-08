FROM --platform=$BUILDPLATFORM golang:1.23 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /app

COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download

COPY . .

RUN ls -alh && CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} GO111MODULE=on go build -a -o karpenter-deprovision-controller .


FROM --platform=$BUILDPLATFORM gcr.io/distroless/static-debian12:nonroot
USER nonroot:nonroot
WORKDIR /app

COPY --from=builder /app/karpenter-deprovision-controller .

ENTRYPOINT ["/app/karpenter-deprovision-controller"]
