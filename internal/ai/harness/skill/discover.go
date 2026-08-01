package skill

import (
	"context"
	"os"
	"path/filepath"

	"github.com/Abraxas-365/manifesto/internal/fsx/fsxlocal"
	"github.com/Abraxas-365/manifesto/internal/logx"
)

// SkillsDirName is the project-relative directory scanned for skills.
// Override at startup to brand it for your product (e.g. ".myagent").
var SkillsDirName = ".agent"

// DiscoverDirs returns the standard skill directories for a project, lowest
// precedence first: user (<userHome>/skills) then project
// (<projectDir>/<SkillsDirName>/skills). Missing dirs are simply absent from
// the result.
func DiscoverDirs(userHome, projectDir string) []string {
	var dirs []string
	for _, d := range []string{
		filepath.Join(userHome, "skills"),
		filepath.Join(projectDir, SkillsDirName, "skills"),
	} {
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			dirs = append(dirs, d)
		}
	}
	return dirs
}

// Discover loads skills from the given directories into a new Registry, in
// order — later dirs override same-named skills from earlier ones (legacy rule:
// project beats user). Malformed skills are skipped with a warning, never an
// error: one bad SKILL.md must not take the agent down.
func Discover(ctx context.Context, dirs ...string) *Registry {
	reg := NewRegistry()
	for _, root := range dirs {
		fs, err := fsxlocal.NewLocalFileSystem(root)
		if err != nil {
			logx.Warnf("skills: cannot open %s: %v", root, err)
			continue
		}
		entries, err := fs.List(ctx, ".")
		if err != nil {
			logx.Warnf("skills: cannot list %s: %v", root, err)
			continue
		}
		for _, e := range entries {
			if !e.IsDir {
				continue
			}
			dir := e.Name
			if ok, err := fs.Exists(ctx, fs.Join(dir, skillFile)); err != nil || !ok {
				continue
			}
			sk, err := FromFS(ctx, fs, dir)
			if err != nil {
				logx.Warnf("skills: skipping %s/%s: %v", root, dir, err)
				continue
			}
			// legacy rule: name defaults to the directory name.
			if sk.Name == "" {
				sk.Name = filepath.Base(dir)
			}
			reg.Register(sk)
		}
	}
	return reg
}
