module github.com/otpki/otpki-pkg

go 1.27.2

require (
	codeberg.org/miekg/dns v0.6.132
	connectrpc.com/connect/v2 v2.0.2
	github.com/apache/pulsar-client-go v0.21.0
	github.com/cloudflare/circl v0.0.0-00010101000000-000000000000
	github.com/dapr/kit v0.19.0
	github.com/google/uuid v1.6.0
	github.com/hashicorp/go-hclog v1.6.3
	github.com/hibiken/asynq v0.26.0
	github.com/jung-kurt/gofpdf v1.16.2
	github.com/miekg/pkcs11 v1.1.2
	github.com/otpki/iogo v0.0.0-20261112152310-36bbc5e4b73f
	github.com/rs/zerolog v1.35.1
	github.com/samber/oops v1.23.2
	github.com/samber/oops/loggers/zerolog v0.0.0-20261101193506-71de68243685
	github.com/skip2/go-qrcode v0.0.0-20200617195104-da1b6568686e
	github.com/smallstep/pkcs7 v0.2.3
	github.com/spf13/cobra v1.10.2
	github.com/spf13/viper v1.21.0
	github.com/stretchr/testify v1.12.1
	github.com/xhit/go-simple-mail/v2 v2.17.0
	go.opentelemetry.io/otel v1.47.0
	go.opentelemetry.io/otel/log v1.47.0
	go.opentelemetry.io/otel/trace v1.47.0
	go.step.sm/crypto v0.92.0
	golang.org/x/crypto v0.59.0
	google.golang.org/grpc v1.85.0
	google.golang.org/protobuf v1.36.12
	gorm.io/gorm v1.31.2
)

require (
	filippo.io/bigmod v0.1.0 // indirect
	filippo.io/edwards25519 v1.2.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/fatih/color v1.19.0 // indirect
	github.com/fsnotify/fsnotify v1.10.1 // indirect
	github.com/go-test/deep v1.1.1 // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/mattn/go-colorable v0.1.16 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/oklog/ulid/v2 v2.1.2 // indirect
	github.com/pelletier/go-toml/v2 v2.4.3 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/redis/go-redis/v9 v9.23.0 // indirect
	github.com/robfig/cron/v3 v3.0.1 // indirect
	github.com/sagikazarmark/locafero v0.12.0 // indirect
	github.com/samber/lo v1.53.0 // indirect
	github.com/sirupsen/logrus v1.10.2 // indirect
	github.com/spf13/afero v1.15.0 // indirect
	github.com/spf13/cast v1.10.0 // indirect
	github.com/spf13/pflag v1.1.0 // indirect
	github.com/subosito/gotenv v1.6.0 // indirect
	github.com/toorop/go-dkim v0.0.0-20250226130143-9025cce95817 // indirect
	go.uber.org/atomic v1.12.0 // indirect
	go.yaml.in/yaml/v3 v3.0.6 // indirect
	golang.org/x/net v0.62.0 // indirect
	golang.org/x/sys v0.50.0 // indirect
	golang.org/x/text v0.44.0 // indirect
	golang.org/x/time v0.16.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20261110000943-09e21b237f2c // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
)

replace github.com/cloudflare/circl => github.com/otpki/circl v0.0.0-20261112151406-a03bc4371af7

replace github.com/smallstep/pkcs7 => github.com/otpki/pkcs7 v0.0.0-20261112150827-3f426f339887
