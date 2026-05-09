# genai-bench Docker Image

genai-bench benchmark tool image, based on [sgl-project/genai-bench](https://github.com/sgl-project/genai-bench).

## Build

```bash
docker build -t <IMG_REPO>/rbgs-genai-bench:<TAG> -f tools/genai/Dockerfile tools/genai/
```

Example:

```bash
IMG_REPO=registry-cn-hangzhou.ack.aliyuncs.com/dev
TAG=v0.7.0

docker build -t ${IMG_REPO}/rbgs-genai-bench:${TAG} -f tools/genai/Dockerfile tools/genai/
```

## Multi-arch Build (buildx)

```bash
docker buildx build --push --platform linux/amd64,linux/arm64 \
  -t ${IMG_REPO}/rbgs-genai-bench:${TAG} \
  -f tools/genai/Dockerfile tools/genai/
```

## Usage

```bash
docker run --rm <IMG_REPO>/rbgs-genai-bench:<TAG> --help
```

The entrypoint is `genai-bench`, all arguments are forwarded directly.
