# Рантайм-образ: бинарник собирается локально в deploy.sh (CGO_ENABLED=0),
# здесь только упаковка.
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata wget \
    && adduser -D -H -u 10001 app \
    && mkdir -p /app/data/backgrounds \
    && chown -R app /app/data

COPY server /app/server
COPY public /app/public

USER app
EXPOSE 8081
ENTRYPOINT ["/app/server"]
