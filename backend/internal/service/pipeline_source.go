package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/source/connection"
	"github.com/santapong/cooker/internal/source/gittransport"
	"github.com/santapong/cooker/internal/store"
)

type PipelineSourceProvider interface {
	AutomationSource
	CheckoutSource(context.Context, *model.PipelineSourcePlan, io.Writer) (string, error)
}

// PipelineSourcePreparer captures exact authority before a native job exists.
type PipelineSourcePreparer struct {
	Store       *store.Store
	Connections *connection.Service
	Source      PipelineSourceProvider
	Head        func(context.Context, string) (string, error)
}

func sourceRelative(p string) bool {
	return !filepath.IsAbs(p) && !strings.ContainsAny(p, "\x00\r\n\\:") && !strings.Contains(p, "${") && !strings.Contains(p, "{{") && !strings.HasPrefix(p, "~")
}

// ValidatePipelineSource defines the initial Build/Push/Docker capability.
// A source-unbound pipeline retains its existing path/adapter semantics.
func ValidatePipelineSource(p *model.Pipeline) error {
	if p == nil {
		return fmt.Errorf("pipeline is required")
	}
	for _, s := range p.Stages {
		if len(s.Config.RegistryConnectionIDs) > 32 {
			return fmt.Errorf("too many registry selections")
		}
		seen := map[string]bool{}
		for _, id := range s.Config.RegistryConnectionIDs {
			if id == "" || len(id) > 128 || strings.ContainsAny(id, "\x00\r\n") || seen[id] {
				return fmt.Errorf("invalid registry selections")
			}
			seen[id] = true
		}
	}
	if p.Source == nil {
		return nil
	}
	s := p.Source
	if s.ConnectionID == "" || len(s.ConnectionID) > 128 || strings.ContainsAny(s.ConnectionID, "\x00\r\n") {
		return fmt.Errorf("approved source connection is required")
	}
	if err := gittransport.ValidateRepositoryPath(s.Repository, true); err != nil {
		return fmt.Errorf("invalid source repository")
	}
	if _, err := parseSourceRef(s.FullRef); err != nil {
		return err
	}
	hasBuild := false
	for _, stage := range p.Stages {
		if stage.Config.ReviewOnly {
			return fmt.Errorf("saved App graphs cannot become source-bound native pipelines")
		}
		switch stage.Type {
		case model.StageTypeBuild:
			hasBuild = true
			c := stage.Config.Context
			if c == "" {
				c = "."
			}
			df := stage.Config.Dockerfile
			if df == "" {
				df = "Dockerfile"
			}
			if !sourceRelative(c) || !safeRelativePath(c) || !sourceRelative(df) || !safeRelativePath(filepath.Join(c, df)) {
				return fmt.Errorf("build paths must remain inside the source repository")
			}
			for _, p := range []string{c, filepath.Join(c, df)} {
				for _, part := range strings.Split(filepath.ToSlash(filepath.Clean(p)), "/") {
					if part == ".git" {
						return fmt.Errorf("Git metadata is not a build input")
					}
				}
			}
		case model.StageTypeDeploy:
			if stage.Config.DeployRuntime != "docker" {
				return fmt.Errorf("source-bound native deployment currently supports Docker only")
			}
		case model.StageTypeTest, model.StageTypePush, model.StageTypeApproval, model.StageTypeCustom:
		default:
			return fmt.Errorf("stage adapter is unavailable for a source-bound pipeline")
		}
	}
	if !hasBuild {
		return fmt.Errorf("source-bound pipeline requires a Build stage")
	}
	return nil
}

func pipelineEnvironmentIDs(p *model.Pipeline) []string {
	set := map[string]bool{}
	for _, stage := range p.Stages {
		if stage.EnvironmentID != "" {
			set[stage.EnvironmentID] = true
		}
	}
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func (s *PipelineSourcePreparer) environments(ctx context.Context, p *model.Pipeline) (map[string]int, error) {
	out := map[string]int{}
	ids := pipelineEnvironmentIDs(p)
	if len(ids) > 100 {
		return nil, fmt.Errorf("too many source-bound environments")
	}
	for _, id := range ids {
		if model.IsRegistryCredentialEnvironmentID(id) {
			return nil, fmt.Errorf("private registry credentials cannot be source environments")
		}
		if s.Store == nil || s.Store.Environments == nil {
			return nil, fmt.Errorf("source environment is unavailable")
		}
		env, err := s.Store.Environments.Get(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("source environment is unavailable")
		}
		if model.IsRegistryCredentialEnvironment(env) {
			return nil, fmt.Errorf("private registry credentials cannot be source environments")
		}
		out[id] = env.Version
	}
	return out, nil
}

func pipelineSourcePlanHash(p *model.Pipeline, plan *model.PipelineSourcePlan) string {
	return model.PipelineSourceConfigDigest(p, plan)
}

// Prepare resolves a typed ref once, inspects exact source, then cleans it up.
// It never deploys, builds, persists credentials, or admits a queue job.
func (s *PipelineSourcePreparer) Prepare(ctx context.Context, p *model.Pipeline, fullRef, policy string) (*model.PipelineSourcePlan, error) {
	if err := ValidatePipelineSource(p); err != nil {
		return nil, err
	}
	if p.Source == nil || p.ID == "" || strings.HasPrefix(p.ID, "vflow_") || strings.HasPrefix(p.ID, "vapp_") {
		return nil, fmt.Errorf("saved source-bound native pipeline is required")
	}
	if policy != "manual" && policy != "automatic" && policy != "review" {
		return nil, fmt.Errorf("invalid source review policy")
	}
	if s == nil || s.Connections == nil || s.Connections.Store == nil || s.Source == nil {
		return nil, fmt.Errorf("pipeline source preparation is unavailable")
	}
	if fullRef == "" {
		fullRef = p.Source.FullRef
	}
	if _, err := parseSourceRef(fullRef); err != nil {
		return nil, err
	}
	envs, err := s.environments(ctx, p)
	if err != nil {
		return nil, err
	}
	c, err := s.Connections.Store.Get(ctx, p.Source.ConnectionID)
	if err != nil || !c.Allows(p.Source.Repository) {
		return nil, fmt.Errorf("source connection does not allow this repository")
	}
	id, err := s.Source.Identity(ctx, c, p.Source.Repository)
	if err != nil || !c.Allows(id.Repository) {
		return nil, fmt.Errorf("cannot verify source repository identity")
	}
	if err = validateTriggerSource(id); err != nil {
		return nil, err
	}
	rev, err := s.Source.Revision(ctx, c, id.Repository, fullRef)
	if err != nil || rev.FullRef != fullRef || gittransport.ValidateCommit(rev.Commit) != nil || rev.Object != "" && gittransport.ValidateCommit(rev.Object) != nil {
		return nil, fmt.Errorf("cannot resolve exact source revision")
	}
	plan := &model.PipelineSourcePlan{Version: 1, Source: id, ConnectionVersion: c.Version, FullRef: fullRef, CommitSHA: rev.Commit, ObjectSHA: rev.Object, PipelineID: p.ID, PipelineVersion: p.Version, EnvironmentVersions: envs, ReviewPolicy: policy}
	plan.PlanHash = pipelineSourcePlanHash(p, plan)
	root, err := s.Source.CheckoutSource(ctx, plan, io.Discard)
	if err != nil {
		return nil, fmt.Errorf("cannot retrieve source for native review")
	}
	defer os.RemoveAll(root)
	head := s.Head
	if head == nil {
		head = gittransport.HeadCommit
	}
	commit, err := head(ctx, root)
	if err != nil || commit != plan.CommitSHA {
		return nil, fmt.Errorf("native checkout does not match accepted commit")
	}
	export, err := ExportPipelineSource(ctx, root)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(export)
	plan.InputFiles, plan.EnvelopeHash, err = InspectPipelineSourceRecipe(p, export)
	if err != nil {
		return nil, err
	}
	return plan, nil
}

// Validate rechecks current authority without choosing a different revision.
// freshness=true is for triggers; manual jobs keep their admitted revision.
func (s *PipelineSourcePreparer) Validate(ctx context.Context, p *model.Pipeline, plan *model.PipelineSourcePlan, freshness bool) error {
	if err := ValidatePipelineSourcePlan(p, plan); err != nil {
		return err
	}
	if s == nil || s.Connections == nil || s.Connections.Store == nil || s.Source == nil {
		return fmt.Errorf("pipeline source validation is unavailable")
	}
	envs, err := s.environments(ctx, p)
	if err != nil || !reflect.DeepEqual(envs, plan.EnvironmentVersions) {
		return fmt.Errorf("source environments changed after admission")
	}
	c, err := s.Connections.Store.Get(ctx, plan.Source.ConnectionID)
	if err != nil || !c.Allows(plan.Source.Repository) || c.Version != plan.ConnectionVersion {
		return fmt.Errorf("source connection changed after admission")
	}
	id, err := s.Source.Identity(ctx, c, plan.Source.Repository)
	if err != nil || !id.SameRepository(plan.Source) || id.Repository != plan.Source.Repository {
		return fmt.Errorf("source repository identity changed after admission")
	}
	if freshness {
		rev, err := s.Source.Revision(ctx, c, plan.Source.Repository, plan.FullRef)
		if err != nil || rev.FullRef != plan.FullRef || rev.Commit != plan.CommitSHA || rev.Object != plan.ObjectSHA {
			return fmt.Errorf("source ref changed after admission")
		}
	}
	return nil
}

func ValidatePipelineSourcePlan(p *model.Pipeline, plan *model.PipelineSourcePlan) error {
	if err := ValidatePipelineSource(p); err != nil {
		return err
	}
	if p.Source == nil || plan == nil || plan.Version != 1 || plan.PipelineID != p.ID || plan.PipelineVersion != p.Version || plan.Source.ConnectionID != p.Source.ConnectionID || plan.Source.Repository != p.Source.Repository || plan.ConnectionVersion < 1 || plan.EnvironmentVersions == nil || len(plan.EnvironmentVersions) > 100 || plan.ReviewPolicy != "manual" && plan.ReviewPolicy != "review" && plan.ReviewPolicy != "automatic" {
		return fmt.Errorf("accepted native source plan does not match pipeline")
	}
	if err := validateTriggerSource(plan.Source); err != nil {
		return err
	}
	if _, err := parseSourceRef(plan.FullRef); err != nil {
		return err
	}
	if gittransport.ValidateCommit(plan.CommitSHA) != nil || plan.ObjectSHA != "" && gittransport.ValidateCommit(plan.ObjectSHA) != nil {
		return fmt.Errorf("invalid accepted source revision")
	}
	ids := pipelineEnvironmentIDs(p)
	if len(ids) != len(plan.EnvironmentVersions) {
		return fmt.Errorf("accepted environment set changed")
	}
	for _, id := range ids {
		version, ok := plan.EnvironmentVersions[id]
		if !ok || version < 0 {
			return fmt.Errorf("accepted environment set changed")
		}
	}
	if plan.PlanHash != pipelineSourcePlanHash(p, plan) || !validHex(plan.EnvelopeHash, 64) || len(plan.InputFiles) == 0 || len(plan.InputFiles) > 384 {
		return fmt.Errorf("accepted source review metadata changed")
	}
	for i, input := range plan.InputFiles {
		if !sourceRelative(input.Path) || !safeRelativePath(input.Path) || input.Path == "." || filepath.ToSlash(filepath.Clean(input.Path)) != input.Path || i > 0 && plan.InputFiles[i-1].Path >= input.Path || input.Present && !validHex(input.SHA256, 64) || !input.Present && input.SHA256 != "" {
			return fmt.Errorf("invalid reviewed source inputs")
		}
	}
	b, _ := json.Marshal(plan.InputFiles)
	hash := sha256.Sum256(b)
	if hex.EncodeToString(hash[:]) != plan.EnvelopeHash {
		return fmt.Errorf("accepted source recipe digest changed")
	}
	return nil
}

func PipelineSourceDigest(plan *model.PipelineSourcePlan) (string, error) {
	return model.PipelineSourcePlanDigest(plan)
}

// ExportPipelineSource removes metadata and rejects links/special entries.
// Caller owns cleanup. Bounds apply to the complete export, not just a context.
func ExportPipelineSource(ctx context.Context, root string) (out string, err error) {
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("source checkout is not a private directory")
	}
	out, err = os.MkdirTemp("", "cooker-native-source-")
	if err != nil {
		return "", err
	}
	owned := out
	defer func() {
		if err != nil {
			os.RemoveAll(owned)
			out = ""
		}
	}()
	entries := 0
	var bytes int64
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("source export is unreadable")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if d.Name() == ".git" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return fmt.Errorf("source metadata is unavailable")
		}
		if rel == "." {
			return nil
		}
		entries++
		if entries > 20000 {
			return fmt.Errorf("source exceeds the 20000-entry export limit")
		}
		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("source export is unreadable")
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("source links and special entries are unsupported")
		}
		dest := filepath.Join(out, rel)
		if info.IsDir() {
			return os.Mkdir(dest, 0700)
		}
		bytes += info.Size()
		if bytes > 1<<30 {
			return fmt.Errorf("source exceeds the 1 GiB export limit")
		}
		in, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("source export is unreadable")
		}
		defer in.Close()
		file, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600|info.Mode().Perm()&0111)
		if err != nil {
			return err
		}
		_, copyErr := io.CopyN(file, in, info.Size())
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	return out, err
}

// PipelineBuildPaths is called on the copied stage after interpolation. No host
// path escapes and no repo link can alter which checkout supplies build bytes.
func PipelineBuildPaths(root string, stage *model.Stage) (contextDir, dockerfile string, err error) {
	if stage == nil || stage.Type != model.StageTypeBuild {
		return "", "", fmt.Errorf("Build stage is required")
	}
	c := stage.Config.Context
	if c == "" {
		c = "."
	}
	df := stage.Config.Dockerfile
	if df == "" {
		df = "Dockerfile"
	}
	if !sourceRelative(c) || !safeRelativePath(c) || !sourceRelative(df) || !safeRelativePath(filepath.Join(c, df)) {
		return "", "", fmt.Errorf("build paths leave accepted source")
	}
	contextDir, err = repositoryPath(root, root, c)
	if err != nil {
		return "", "", fmt.Errorf("source build context is unavailable")
	}
	info, err := os.Stat(contextDir)
	if err != nil || !info.IsDir() {
		return "", "", fmt.Errorf("source build context is not a directory")
	}
	path, err := repositoryPath(root, contextDir, df)
	if err != nil {
		return "", "", fmt.Errorf("source Dockerfile is unavailable")
	}
	info, err = os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", "", fmt.Errorf("source Dockerfile is not regular")
	}
	dockerfile, err = filepath.Rel(contextDir, path)
	return
}

func InspectPipelineSourceRecipe(p *model.Pipeline, root string) ([]model.SourceInputHash, string, error) {
	files := map[string]bool{}
	for i := range p.Stages {
		stage := &p.Stages[i]
		if stage.Type != model.StageTypeBuild {
			continue
		}
		contextDir, df, err := PipelineBuildPaths(root, stage)
		if err != nil {
			return nil, "", err
		}
		absDF := filepath.Join(contextDir, df)
		for _, name := range []string{absDF, filepath.Join(contextDir, ".dockerignore"), absDF + ".dockerignore"} {
			rel, err := filepath.Rel(root, name)
			if err != nil {
				return nil, "", err
			}
			files[filepath.ToSlash(rel)] = true
		}
	}
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	if len(paths) > 384 {
		return nil, "", fmt.Errorf("too many source recipe inputs")
	}
	inputs := make([]model.SourceInputHash, 0, len(paths))
	for _, path := range paths {
		input := model.SourceInputHash{Path: path}
		data, err := readSourceFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			if _, statErr := os.Lstat(filepath.Join(root, filepath.FromSlash(path))); !os.IsNotExist(statErr) {
				return nil, "", fmt.Errorf("source recipe is unavailable")
			}
		} else {
			input.Present = true
			h := sha256.Sum256(data)
			input.SHA256 = hex.EncodeToString(h[:])
		}
		inputs = append(inputs, input)
	}
	b, _ := json.Marshal(inputs)
	hash := sha256.Sum256(b)
	return inputs, hex.EncodeToString(hash[:]), nil
}
