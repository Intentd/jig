package expect

import "github.com/intentd/jig/pkg/spec"

func Call(name, url string) spec.ExternalCheck {
	c := spec.ExternalCheck{}
	c.Name, c.URL = name, url
	return c
}
