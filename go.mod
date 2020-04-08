module github.com/Dynom/ERI

go 1.14

require (
	cloud.google.com/go v0.56.0
	cloud.google.com/go/pubsub v1.3.1
	github.com/BurntSushi/toml v0.3.1
	github.com/Dynom/TySug v0.1.3
	github.com/NYTimes/gziphandler v1.1.1
	github.com/Pimmr/rig v0.0.0-20200327123708-a6d34f8b4a0b
	github.com/alextanhongpin/stringdist v0.0.1 // indirect
	github.com/graphql-go/graphql v0.7.9
	github.com/graphql-go/handler v0.2.3
	github.com/juju/ratelimit v1.0.1
	github.com/konsorten/go-windows-terminal-sequences v1.0.2 // indirect
	github.com/kr/pretty v0.2.0 // indirect
	github.com/minio/highwayhash v1.0.0
	github.com/pkg/errors v0.9.1 // indirect
	github.com/rs/cors v1.7.0
	github.com/sirupsen/logrus v1.5.0
	golang.org/x/sys v0.0.0-20200408040146-ea54a3c99b9b // indirect
	golang.org/x/tools v0.0.0-20200408032209-46bd65c8538f // indirect
	google.golang.org/api v0.21.0
	google.golang.org/genproto v0.0.0-20200407120235-9eb9bb161a06 // indirect
	google.golang.org/grpc v1.28.1 // indirect
	gopkg.in/check.v1 v1.0.0-20190902080502-41f04d3bba15 // indirect
	gopkg.in/yaml.v2 v2.2.8 // indirect
)

//replace github.com/Dynom/TySug => ../TySug
