package pkg

import (
	"bytes"
	"errors"
	"fmt"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/go-git/go-git/v5"
	"gopkg.in/yaml.v3"
	"io"
	"regexp"
	"sigs.k8s.io/kustomize/api/types"
	"strings"
	"text/template"
)

type Deployment struct {
	Name            string
	RepositoryName  string
	KustomizePath   string
	CommitMessage   *template.Template
	Images          []string
	ApplicationName string
}

var errorNoModification = errors.New("no changes made")

func NewDeployment(cfg DeploymentConfig) (*Deployment, error) {
	toRet := &Deployment{
		Name:            cfg.Name,
		RepositoryName:  cfg.Repository,
		KustomizePath:   cfg.Path,
		Images:          cfg.Images,
		ApplicationName: cfg.ArgoName,
	}
	if toRet.KustomizePath == "" {
		toRet.KustomizePath = "kustomization.yaml"
	}
	if cfg.CommitMessage == "" {
		cfg.CommitMessage = "[{{ .name }}] Version bumped to {{ .tag }} by {{ .user }}"
	}
	tpl := template.New("")
	if _, err := tpl.Parse(cfg.CommitMessage); err != nil {
		return nil, fmt.Errorf("failed to parse message template: %w", err)
	}
	toRet.CommitMessage = tpl

	return toRet, nil
}

func (d Deployment) Apply(worktree *git.Worktree, newTag string, user string) (string, error) {
	// Start by reading the kustomization file
	inFile, err := worktree.Filesystem.Open(d.KustomizePath)
	if err != nil {
		return "", fmt.Errorf("failed to open kustomization file: %w", err)
	}
	defer inFile.Close()
	kustomizationBytes, err := io.ReadAll(inFile)
	if err != nil {
		return "", fmt.Errorf("failed to read kustomization file: %w", err)
	}

	// Then unmarshal it so that we have a source of truth to work from
	var kustomization types.Kustomization
	err = yaml.Unmarshal(kustomizationBytes, &kustomization)
	if err != nil {
		return "", fmt.Errorf("failed to decode kustomization file: %w", err)
	}
	// Also convert the bytes to a string
	kustomizationString := string(kustomizationBytes[:])

	// Keep track of what images should be found, and whether we've made changes at all
	changeMade := false
	wantedImages := mapset.NewThreadUnsafeSet[string]()
	for _, im := range d.Images {
		if !strings.ContainsRune(im, '*') {
			wantedImages.Add(im)
		}
	}
	// Loop over the deployment's images, replacing their tags
	for _, im := range kustomization.Images {
		if !matchImage(d.Images, im.Name) {
			continue
		}
		wantedImages.Remove(im.Name)
		if newKustomizationString, err := changeTag(kustomizationString, im.Name, newTag); err != nil {
			return "", fmt.Errorf("failed to replace image %s: %w", im.Name, err)
		} else {
			changeMade = newKustomizationString != kustomizationString
			kustomizationString = newKustomizationString
		}
	}
	if !wantedImages.IsEmpty() {
		return "", fmt.Errorf("kustomization file does not contain image(s): %s", strings.Join(wantedImages.ToSlice(), ", "))
	}
	if !changeMade {
		return "", errorNoModification
	}

	// Write it back and stage the file for commit
	outFile, err := worktree.Filesystem.Create(d.KustomizePath)
	_, err = outFile.Write([]byte(kustomizationString))
	if err != nil {
		_ = outFile.Close()
		return "", fmt.Errorf("failed to write kustomization file: %w", err)
	}
	_ = outFile.Close()
	_, err = worktree.Add(d.KustomizePath)
	if err != nil {
		return "", fmt.Errorf("failed to stage kustomization file: %w", err)
	}

	// Commit the change
	commitMsg := bytes.Buffer{}
	if err := d.CommitMessage.Execute(&commitMsg, map[string]string{
		"name": d.Name,
		"tag":  newTag,
		"user": user,
	}); err != nil {
		return "", fmt.Errorf("failed to execute message template: %w", err)
	}
	commitHash, err := worktree.Commit(commitMsg.String(), &git.CommitOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to commit kustomization file: %w", err)
	}

	return commitHash.String(), nil
}

func fnmatch(pattern string, input string) bool {
	// Shortcut for when no globbing is required
	if !strings.ContainsRune(pattern, '*') {
		return input == pattern
	}
	// Make sure everything but the *s are quoted
	parts := strings.Split(pattern, "*")
	quotedParts := make([]string, 0, len(parts))
	for _, part := range parts {
		quotedParts = append(quotedParts, regexp.QuoteMeta(part))
	}
	// And construct and match the final regex
	// NB: String must be anchored because re.Match is really a search
	finalPattern := "^" + strings.Join(quotedParts, ".*") + "$"
	re := regexp.MustCompile(finalPattern)
	return re.MatchString(input)
}

func matchImage(images []string, search string) bool {
	for _, image := range images {
		if fnmatch(image, search) {
			return true
		}
	}

	return false
}

func changeTag(kustomizeBody string, imageName string, newTag string) (string, error) {
	// To use the image name in the regex, we first have to quote it
	quotedName := regexp.QuoteMeta(imageName)
	// Substitute the name in and compile the regex
	reTpl := `(?ms)^\s*-\s+name:\s+["']?%s["']?$.+?^\s+newTag:\s+["']?([^"'$]+?)["']?$`
	re, err := regexp.Compile(fmt.Sprintf(reTpl, quotedName))
	if err != nil {
		return "", fmt.Errorf("failed to compile regex: %w", err)
	}
	// We only want 1 match, but search for 2 so we can detect duplicates
	matches := re.FindAllStringSubmatchIndex(kustomizeBody, 2)
	if len(matches) == 0 {
		return "", fmt.Errorf("could not find image definition for %s", imageName)
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("found more than one image definition for %s", imageName)
	}
	// Finally, use the indexes from the match to construct the name body
	match := matches[0]
	tagStart := match[2]
	tagEnd := match[3]
	return kustomizeBody[0:tagStart] + newTag + kustomizeBody[tagEnd:], nil
}
