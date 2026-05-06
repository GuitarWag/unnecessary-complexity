module github.com/yld/url-shortener/services/analytics

go 1.26

require (
	github.com/mattn/go-sqlite3 v1.14.44
	github.com/stretchr/testify v1.10.0
	github.com/yld/url-shortener/services/platform-events v0.0.0-00010101000000-000000000000
	github.com/yld/url-shortener/services/proto v0.0.0-00010101000000-000000000000
	google.golang.org/protobuf v1.36.11
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/klauspost/compress v1.15.9 // indirect
	github.com/pierrec/lz4/v4 v4.1.15 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/segmentio/kafka-go v0.4.47 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace (
	github.com/yld/url-shortener/services/platform-events => ../platform-events
	github.com/yld/url-shortener/services/proto => ../proto
)
