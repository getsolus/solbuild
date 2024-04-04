package builder

import (
	"cmp"
	"errors"
	"fmt"
	"slices"

	"github.com/getsolus/libeopkg/index"
)

type Resolver struct {
	// indices   []Index
	providers map[string]string
	nameToPkg map[string]index.Package
}

type Dep struct {
	Name string `json:"name"`
	Hash string `json:"hash"`
}

func NewResolver() (res *Resolver) {
	res = &Resolver{
		providers: make(map[string]string),
		nameToPkg: make(map[string]index.Package),
	}
	return
}

func (r *Resolver) AddIndex(i *index.Index) {
	for _, pkg := range i.Packages {
		if _, ok := r.nameToPkg[pkg.Name]; !ok {
			r.nameToPkg[pkg.Name] = pkg
		}

		if pkg.Provides != nil {
			for _, provides := range pkg.Provides.PkgConfig {
				provider := fmt.Sprintf("pkgconfig(%s)", provides)
				if _, ok := r.providers[provider]; !ok {
					r.providers[provider] = pkg.Name
				}
			}
			for _, provides := range pkg.Provides.PkgConfig32 {
				provider := fmt.Sprintf("pkgconfig32(%s)", provides)
				if _, ok := r.providers[provider]; !ok {
					r.providers[provider] = pkg.Name
				}
			}
		}
	}
}

func (r *Resolver) Query(pkgs []string, withBase bool, withDevel bool) (res []Dep, err error) {
	visited := make(map[string]bool)

	var dfs func(name string) error
	dfs = func(name string) error {
		if _, ok := r.providers[name]; ok {
			name = r.providers[name]
		}

		if visited[name] {
			return nil
		}

		if _, ok := r.nameToPkg[name]; !ok {
			return errors.New("Unable to find provider or package " + name)
		}
		visited[name] = true

		pkg := r.nameToPkg[name]
		res = append(res, Dep{Name: pkg.Name, Hash: pkg.PackageHash})
		for _, dep := range r.nameToPkg[name].RuntimeDependencies {
			err = dfs(dep.Name)
			if err != nil {
				return err
			}
		}

		return nil
	}

	if withBase || withDevel {
		for _, pkg := range r.nameToPkg {
			if withBase && pkg.PartOf == "system.base" {
				dfs(pkg.Name)
			} else if withDevel && pkg.PartOf == "system.devel" {
				dfs(pkg.Name)
			}
		}
	}

	for _, pkg := range pkgs {
		err = dfs(pkg)
		if err != nil {
			return
		}
	}

	slices.SortFunc(res, func(a, b Dep) int { return cmp.Compare(a.Name, b.Name) })
	return
}
