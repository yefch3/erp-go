# The frontend as a shippable artifact: dist baked into nginx.
# Build context is the REPOSITORY ROOT, same convention as deploy/Dockerfile.
#
# In production this container binds 127.0.0.1:8081 and the host nginx
# (which owns 443 and the certificates) proxies / here and /api to the
# gateway. It serves static files and nothing else — no TLS, no proxying,
# no knowledge of the backend.
FROM node:22-alpine AS build
WORKDIR /src
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM nginx:1.27-alpine
COPY deploy/frontend-nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /src/dist /usr/share/nginx/html
