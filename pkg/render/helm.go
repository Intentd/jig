package render

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/intentd/jig/pkg/spec"
	"sigs.k8s.io/yaml"
)

// renderHelmInstall renders Helm value files in their declared order,
// followed by an override file for inline values.
func renderHelmInstall(installs spec.HelmInstalls, baseDir string) (spec.RenderFiles, error) {
	var files spec.RenderFiles

	for i, install := range installs {
		dir := filepath.Join("helm-install", fmt.Sprintf("%02d", i))

		for j, valueFile := range install.ValueFiles {
			if valueFile == "" {
				continue
			}

			parsed, err := url.Parse(valueFile)
			if err != nil {
				return nil, fmt.Errorf(
					"parse Helm value file %q for install %d: %w",
					valueFile, i, err,
				)
			}

			// Remote value files remain remote references.
			if parsed.IsAbs() && parsed.Host != "" {
				continue
			}

			source := valueFile
			if !filepath.IsAbs(source) {
				source = filepath.Join(baseDir, source)
			}

			data, err := os.ReadFile(source)
			if err != nil {
				return nil, fmt.Errorf(
					"read Helm value file %q for install %d: %w",
					valueFile, i, err,
				)
			}

			files = append(files, spec.RenderFile{
				Path:    filepath.Join(dir, fmt.Sprintf("values-%02d.yaml", j)),
				Content: data,
				YAML:    true,
			})
		}

		if len(install.Values) == 0 {
			continue
		}

		values, err := yaml.Marshal(install.Values)
		if err != nil {
			return nil, fmt.Errorf("marshal Helm values for install %d: %w", i, err)
		}

		var content strings.Builder
		content.WriteString("# Helm install\n")
		content.WriteString("# Repository: ")
		content.WriteString(install.Repo)
		content.WriteString("\n# Chart: ")
		content.WriteString(install.Chart)
		content.WriteString("\n# Release: ")
		content.WriteString(install.ReleaseName())
		content.WriteString("\n# Namespace: ")
		content.WriteString(install.EffectiveNamespace())
		content.WriteString("\n")

		if install.Version != "" {
			content.WriteString("# Version: ")
			content.WriteString(install.Version)
			content.WriteString("\n")
		}

		content.WriteString("\n")
		content.Write(values)

		files = append(files, spec.RenderFile{
			Path:    filepath.Join(dir, "values-override.yaml"),
			Content: []byte(content.String()),
			YAML:    true,
		})
	}

	return files, nil
}
