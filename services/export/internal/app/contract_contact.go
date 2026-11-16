package app

import "strings"

func contactSnapshot(name string) *string {
	value := strings.TrimSpace(name)
	return &value
}

func contactValue(name *string) string {
	if name == nil {
		return ""
	}
	return *name
}

func inheritedContact(name string, previous *string) *string {
	if strings.TrimSpace(name) == "" {
		return previous
	}
	return contactSnapshot(name)
}
