# genai-bench Docker Image

genai-bench benchmark tool image, based on [sgl-project/genai-bench](https://github.com/sgl-project/genai-bench).

## Build

```bash
docker build -t <IMG_REPO>/rbgs-benchmark-tool-genai:<TAG> -f tools/genai/Dockerfile tools/genai/
```

Example:

```bash
IMG_REPO=registry-cn-hangzhou.ack.aliyuncs.com/dev
TAG=<TAG>

docker build -t ${IMG_REPO}/rbgs-benchmark-tool-genai:${TAG} -f tools/genai/Dockerfile tools/genai/
```

## Multi-arch Build (buildx)

```bash
docker buildx build --push --platform linux/amd64,linux/arm64 \
  -t ${IMG_REPO}/rbgs-benchmark-tool-genai:${TAG} \
  -f tools/genai/Dockerfile tools/genai/
```

## Usage

```bash
docker run --rm <IMG_REPO>/rbgs-benchmark-tool-genai:<TAG> --help
```

The entrypoint is `genai-bench`, all arguments are forwarded directly.
