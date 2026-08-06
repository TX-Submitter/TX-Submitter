module github.com/gamp/stellar-tx-submitter

go 1.23

require (
	github.com/jackc/pgx/v5 v5.7.2
	github.com/prometheus/client_golang v1.20.0
	github.com/shopspring/decimal v1.4.0
	github.com/stellar/go-stellar-sdk v0.7.1
)

require (
	golang.org/x/crypto v0.45.0 // indirect
	golang.org/x/sync v0.18.0 // indirect
	golang.org/x/sys v0.38.0 // indirect
	golang.org/x/text v0.31.0 // indirect
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/go-chi/chi v4.1.2+incompatible // indirect
	github.com/go-errors/errors v1.5.1 // indirect
	github.com/gorilla/schema v1.4.1 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/klauspost/compress v1.17.9 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/manucorporat/sse v0.0.0-20160126180136-ee05b128a739 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/prometheus/client_model v0.6.1 // indirect
	github.com/prometheus/common v0.55.0 // indirect
	github.com/prometheus/procfs v0.15.1 // indirect
	github.com/segmentio/go-loggly v0.5.1-0.20171222203950-eb91657e62b2 // indirect
	github.com/sirupsen/logrus v1.9.3 // indirect
	github.com/stellar/go-xdr v0.0.0-20260529210834-0bf8f4956364 // indirect
	github.com/stretchr/objx v0.5.2 // indirect
	github.com/stretchr/testify v1.10.0 // indirect
	golang.org/x/exp v0.0.0-20231006140011-7918f672742d // indirect
	golang.org/x/net v0.47.0
	google.golang.org/protobuf v1.34.2 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace (
	github.com/stellar/go-stellar-sdk => ./vendor-stellar/go-stellar-sdk
	github.com/stellar/go-xdr => ./vendor-stellar/go-xdr
	golang.org/x/crypto => golang.org/x/crypto v0.31.0
	golang.org/x/net => golang.org/x/net v0.33.0
	golang.org/x/sync => golang.org/x/sync v0.10.0
	golang.org/x/sys => golang.org/x/sys v0.28.0
	golang.org/x/text => golang.org/x/text v0.21.0
)
