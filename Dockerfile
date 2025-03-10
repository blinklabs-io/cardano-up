FROM ghcr.io/blinklabs-io/go:1.24.1-1 AS build

WORKDIR /code
COPY . .
RUN make build

FROM cgr.dev/chainguard/glibc-dynamic AS cardano-up
COPY --from=build /code/cardano-up /bin/
ENTRYPOINT ["cardano-up"]
