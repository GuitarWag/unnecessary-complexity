module github.com/yld/url-shortener/services/gateway

go 1.26

require (
	connectrpc.com/connect v1.18.1
	github.com/rs/cors v1.11.1
	github.com/stretchr/testify v1.10.0
	github.com/yld/url-shortener/services/platform-events v0.0.0-00010101000000-000000000000
	github.com/yld/url-shortener/services/proto v0.0.0-00010101000000-000000000000
	golang.org/x/net v0.51.0
	google.golang.org/grpc v1.81.0
	google.golang.org/protobuf v1.36.11
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/klauspost/compress v1.17.9 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/prometheus/client_golang v1.20.5 // indirect
	github.com/prometheus/client_model v0.6.1 // indirect
	github.com/prometheus/common v0.55.0 // indirect
	github.com/prometheus/procfs v0.15.1 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/text v0.34.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260226221140-a57be14db171 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace (
	github.com/yld/url-shortener/services/platform-events => ../platform-events
	github.com/yld/url-shortener/services/proto => ../proto
)
