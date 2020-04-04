package pubsub

import "github.com/Dynom/ERI/validator/validations"

type Notification struct {
	SenderID string `json:"sid"`
	Data     Data   `json:"data"`
}

type Data struct {
	Local       string                  `json:"local"`
	Domain      string                  `json:"domain"`
	Validations validations.Validations `json:"v"`
	Steps       validations.Steps       `json:"s"`
}
