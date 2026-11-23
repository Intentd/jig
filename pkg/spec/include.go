package spec

import (
	"github.com/intentd/tools/fileio"
	"github.com/intentd/tools/structured"
)

var (
	readLocal       = fileio.ReadLocal
	strictUnmarshal = structured.StrictUnmarshal
)

// ExpectInclude is the file an include: entry in an E2E suite's expect list
// names: the expectations that replace the entry, run from the file's
// directory.
type ExpectInclude struct {
	Expect Expectations `yaml:"expect"`
}

// IsExpectIncludeDoc returns true when the file is a valid E2E expect include.
func IsExpectIncludeDoc(path string) (bool, error) {
	data, err := readLocal(path)
	if err != nil {
		return false, err
	}

	var include ExpectInclude
	if err := strictUnmarshal(data, &include); err != nil {
		return false, err
	}

	return true, nil
}

// ExternalInclude is the file an include: entry in an expectation's
// external checks names: the checks that replace the entry.
type ExternalInclude struct {
	Calls ExternalChecks `yaml:"calls"`
}

// IsExternalIncludeDoc returns true when the file is a valid E2E external check include.
func IsExternalIncludeDoc(path string) (bool, error) {
	data, err := readLocal(path)
	if err != nil {
		return false, err
	}

	var include ExternalInclude
	if err := strictUnmarshal(data, &include); err != nil {
		return false, err
	}

	return true, nil
}
