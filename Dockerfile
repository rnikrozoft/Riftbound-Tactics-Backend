FROM heroiclabs/nakama-pluginbuilder:3.37.0 AS builder
WORKDIR /backend
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go test -race ./... && go build --trimpath --buildmode=plugin -o /backend/riftbound.so .
FROM heroiclabs/nakama:3.37.0
COPY --from=builder /backend/riftbound.so /nakama/data/modules/riftbound.so
COPY local.yml /nakama/data/local.yml
