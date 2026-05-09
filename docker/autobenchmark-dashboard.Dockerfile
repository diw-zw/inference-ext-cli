# Stage 1: build React app
FROM node:20-alpine AS builder
WORKDIR /app

# Install dependencies (layer caching)
COPY ./ui/auto-benchmark/package.json ./ui/auto-benchmark/package-lock.json ./
RUN npm ci

# Build
COPY ./ui/auto-benchmark .
RUN npm run build

# Stage 2: nginx serve
FROM nginx:alpine

# Copy built static files
COPY --from=builder /app/dist /usr/share/nginx/html

EXPOSE 80
CMD ["nginx", "-g", "daemon off;"]
