module github.com/Dynom/ERI

go 1.13

require (
	github.com/BurntSushi/toml v0.3.1
	github.com/Dynom/TySug v0.1.3-0.20190501140824-4748e35329ec
	github.com/NYTimes/gziphandler v1.0.1
	github.com/graphql-go/graphql v0.7.9
	github.com/graphql-go/handler v0.2.3
	github.com/juju/ratelimit v1.0.1
	github.com/kr/pretty v0.2.0 // indirect
	github.com/minio/highwayhash v1.0.0
	github.com/sirupsen/logrus v1.4.2
	github.com/stretchr/testify v1.4.0 // indirect
	golang.org/x/sys v0.0.0-20200122134326-e047566fdf82 // indirect
	gopkg.in/check.v1 v1.0.0-20190902080502-41f04d3bba15 // indirect
	gopkg.in/yaml.v2 v2.2.4 // indirect
)

replace github.com/Dynom/TySug => ../TySug
