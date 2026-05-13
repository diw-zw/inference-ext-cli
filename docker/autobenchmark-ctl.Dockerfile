# Build the auto-benchmark controller binary
FROM golang:1.26 AS builder

ARG GOPROXY
ARG GOPRIVATE
ARG GOSUMDB
ARG TARGETARCH
ARG TARGETOS

ENV GOPROXY=${GOPROXY} \
    GOPRIVATE=${GOPRIVATE} \
    GOSUMDB=${GOSUMDB}

WORKDIR /workspace
ADD . /workspace

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build -o autobenchmark ./cmd/autobenchmark/

# Final image: Python base with genai-bench + controller binary
FROM python:3.12.12-slim

# Install runtime libraries required by numerical Python wheels (numpy, scipy, etc.)
RUN apt-get update && apt-get install -y --no-install-recommends \
    libgomp1 \
    && rm -rf /var/lib/apt/lists/*

# Install genai-bench and optuna from PyPI
RUN pip install --no-cache-dir --upgrade pip && \
    pip install --no-cache-dir genai-bench optuna

# Copy optuna bridge script
COPY tools/optuna/optuna_bridge.py /tools/optuna_bridge.py

# Copy controller binary
WORKDIR /
COPY --from=builder --chmod=755 /workspace/autobenchmark .

ENTRYPOINT ["/autobenchmark"]
