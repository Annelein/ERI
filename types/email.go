package types

import (
	"errors"
	"strings"
)

var (
	ErrInvalidEmailAddress = errors.New("invalid e-mail address, address is missing @")
)

// NewEmailFromParts reconstructs EmailParts from two parts
func NewEmailFromParts(local, domain string) EmailParts {
	return EmailParts{
		Address: local + "@" + domain,
		Local:   local,
		Domain:  domain,
	}
}

// NewEmailParts takes an e-mail address and returns it lower-cased and in parts. It performs only the most minimal form
// of syntax validation. An error is returned when the address doesn't contain an @, or when the input size is abnormal.
func NewEmailParts(emailAddress string) (EmailParts, error) {
	p, err := splitLocalAndDomain(emailAddress)
	if err != nil {
		return EmailParts{}, err
	}

	return p, nil
}

type EmailParts struct {
	Address string
	Local   string
	Domain  string
}

func splitLocalAndDomain(input string) (EmailParts, error) {
	var i int

	if len(input) <= 253 {
		i = strings.LastIndex(input, "@")
	}

	if 0 >= i || i >= len(input) {
		return EmailParts{}, ErrInvalidEmailAddress
	}

	return EmailParts{
		Address: input,
		Local:   input[:i],
		Domain:  input[i+1:],
	}, nil
}
