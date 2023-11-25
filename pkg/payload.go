package pkg

import (
	"errors"
	"fmt"
	"strings"
)

type webhookPayload struct {
	Deployment   string `json:"deployment"`
	TagName      string `json:"tag_name"`
	AuthorizedBy string `json:"authorized_by"`
}

//goland:noinspection GoErrorStringFormat
var missingFieldError = errors.New("Nissing field")

//goland:noinspection GoErrorStringFormat
var invalidFieldError = errors.New("Invalid field")

func (p webhookPayload) Validate() error {
	if p.Deployment == "" {
		return fmt.Errorf("%w: deployment", missingFieldError)
	}
	if p.TagName == "" {
		return fmt.Errorf("%w: tag_name", missingFieldError)
	}
	if p.AuthorizedBy == "" {
		return fmt.Errorf("%w: authorized_by", missingFieldError)
	}
	if strings.Contains(p.TagName, " ") {
		return fmt.Errorf("%w: tag_name", invalidFieldError)
	}

	return nil
}
