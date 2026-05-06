module github.com/yld/url-shortener/services/platform-events

go 1.26

require (
	github.com/segmentio/kafka-go v0.4.47
	github.com/stretchr/testify v1.10.0
	github.com/yld/url-shortener/services/proto v0.0.0-00010101000000-000000000000
	google.golang.org/protobuf v1.36.11
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/klauspost/compress v1.15.9 // indirect
	github.com/pierrec/lz4/v4 v4.1.15 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/yld/url-shortener/services/proto => ../proto
