//
// Copyright © 2016-2021 Solus Project <copyright@getsol.us>
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

package builder

import (
	"encoding/xml"
	"errors"
	"fmt"
	git "github.com/libgit2/git2go/v33"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

const (
	// MaxChangelogEntries is the absolute maximum number of entries we'll
	// parse and provide changelog entries for.
	MaxChangelogEntries = 10

	// UpdateDateFormat is the time format we emit in the history.xml, i.e.
	// 2016-09-24
	UpdateDateFormat = "2006-01-02"
)

var (
	// CveRegex is used to identify security updates which mention a specific
	// CVE ID.
	CveRegex *regexp.Regexp
)

func init() {
	CveRegex = regexp.MustCompile(`(CVE\-[0-9]+\-[0-9]+)`)
}

// PackageHistory is an automatic changelog generated from the changes to
// the package.yml file during the history of the package.
//
// Through this system, we provide a `history.xml` file to `ypkg-build`
// inside the container, which allows it to export the changelog back to
// the user.
//
// This provides a much more natural system than having dedicated changelog
// files in package gits, as it reduces any and all duplication.
// We also have the opportunity to parse natural elements from the git history
// to make determinations as to the update *type*, such as a security update,
// or an update that requires a reboot to the users system.
//
// Currently we're only scoping for security update notification, though
// more features will come in time.
type PackageHistory struct {
	Updates []*PackageUpdate

	pkgfile string // Path of the package
}

// A PackageUpdate is a point in history in the git changes, which is parsed
// from a git.Commit
type PackageUpdate struct {
	Tag         string    // The associated git tag
	Author      string    // The author name of the change
	AuthorEmail string    // The author email of the change
	Body        string    // The associated message of the commit
	Time        time.Time // When the update took place
	ObjectID    string    // OID stored in string form
	Package     *Package  // Associated parsed package
	IsSecurity  bool      // Whether this is a security update
}

// NewPackageUpdate will attempt to parse the given commit and provide a usable
// entry for the PackageHistory
func NewPackageUpdate(tag string, commit *git.Commit, objectID string) *PackageUpdate {
	signature := commit.Author()
	update := &PackageUpdate{Tag: tag}

	// We duplicate. cgo makes life difficult.
	update.Author = signature.Name
	update.AuthorEmail = signature.Email
	update.Body = commit.Message()
	update.Time = signature.When
	update.ObjectID = objectID

	// Attempt to identify the update type. Limit to 1 match, we only need to
	// know IF there is a CVE fix, not how many.
	cves := CveRegex.FindAllString(update.Body, 1)
	if len(cves) > 0 {
		update.IsSecurity = true
	}

	return update
}

// CatGitBlob will return the contents of the given entry
func CatGitBlob(repo *git.Repository, entry *git.TreeEntry) ([]byte, error) {
	obj, err := repo.Lookup(entry.Id)
	if err != nil {
		return nil, err
	}
	blob, err := obj.AsBlob()
	if err != nil {
		return nil, err
	}
	return blob.Contents(), nil
}

// GetFileContents will attempt to read the entire object at path from
// the given tag, within that repo.
func GetFileContents(repo *git.Repository, tag, path string) ([]byte, error) {
	oid, err := git.NewOid(tag)
	if err != nil {
		return nil, err
	}
	commit, err := repo.Lookup(oid)
	if err != nil {
		return nil, err
	}
	treeObj, err := commit.Peel(git.ObjectTree)
	if err != nil {
		return nil, err
	}
	tree, err := treeObj.AsTree()
	if err != nil {
		return nil, err
	}
	entry, err := tree.EntryByPath(path)
	if err != nil {
		return nil, err
	}

	return CatGitBlob(repo, entry)
}

// NewPackageHistory will attempt to analyze the git history at the given
// repository path, and return a usable instance of PackageHistory for writing
// to the container history.xml file.
//
// The repository path will be taken as the directory name of the pkgfile that
// is given to this function.
func NewPackageHistory(pkgfile string) (*PackageHistory, error) {
	// Repodir
	path := filepath.Dir(pkgfile)

	repo, err := git.OpenRepository(path)
	if err != nil {
		return nil, err
	}
	// Get all the tags
	var tags []string
	tags, err = repo.Tags.List()
	if err != nil {
		return nil, err
	}

	updates := make(map[string]*PackageUpdate)

	// Iterate all of the tags
	err = repo.Tags.Foreach(func(name string, id *git.Oid) error {
		if name == "" || id == nil {
			return nil
		}

		var commit *git.Commit

		obj, err := repo.Lookup(id)
		if err != nil {
			return err
		}

		switch obj.Type() {
		// Unannotated tag
		case git.ObjectCommit:
			commit, err = obj.AsCommit()
			if err != nil {
				return err
			}
			tags = append(tags, name)
		// Annotated tag with commit target
		case git.ObjectTag:
			tag, err := obj.AsTag()
			if err != nil {
				return err
			}
			commit, err = repo.LookupCommit(tag.TargetId())
			if err != nil {
				return err
			}
			tags = append(tags, name)
		default:
			return fmt.Errorf("Internal git error, found %s", obj.Type().String())
		}
		if commit == nil {
			return nil
		}
		commitObj := NewPackageUpdate(name, commit, id.String())
		updates[name] = commitObj
		return nil
	})
	// Foreach went bork
	if err != nil {
		return nil, err
	}
	// Sort the tags by -refname
	sort.Sort(sort.Reverse(sort.StringSlice(tags)))

	ret := &PackageHistory{pkgfile: pkgfile}
	ret.scanUpdates(repo, updates, tags)
	updates = nil

	if len(ret.Updates) < 1 {
		return nil, errors.New("No usable git history found")
	}

	// All done!
	return ret, nil
}

// SortUpdatesByRelease is a simple wrapper to allowing sorting history
type SortUpdatesByRelease []*PackageUpdate

func (a SortUpdatesByRelease) Len() int {
	return len(a)
}

func (a SortUpdatesByRelease) Swap(i, j int) {
	a[i], a[j] = a[j], a[i]
}

func (a SortUpdatesByRelease) Less(i, j int) bool {
	return a[i].Package.Release < a[j].Package.Release
}

// scanUpdates will go back through the collected, "ok" tags, and analyze
// them to be more useful.
func (p *PackageHistory) scanUpdates(repo *git.Repository, updates map[string]*PackageUpdate, tags []string) {
	// basename of file
	fname := filepath.Base(p.pkgfile)

	var updateSet []*PackageUpdate
	// Iterate the commit set in order
	for _, tagID := range tags {
		update := updates[tagID]
		if update == nil {
			continue
		}
		b, err := GetFileContents(repo, update.ObjectID, fname)
		if err != nil {
			continue
		}

		var pkg *Package
		// Shouldn't *actually* bail here. Malformed packages do happen
		if pkg, err = NewYmlPackageFromBytes(b); err != nil {
			continue
		}
		update.Package = pkg
		updateSet = append(updateSet, update)
	}
	sort.Sort(sort.Reverse(SortUpdatesByRelease(updateSet)))
	if len(updateSet) >= MaxChangelogEntries {
		p.Updates = updateSet[:MaxChangelogEntries]
	} else {
		p.Updates = updateSet
	}

}

// YPKG provides ypkg-gen-history history.xml compatibility
type YPKG struct {
	History []*YPKGUpdate `xml:">Update"`
}

// YPKGUpdate represents an update in the package history
type YPKGUpdate struct {
	Release int    `xml:"release,attr"`
	Type    string `xml:"type,attr,omitempty"`
	Date    string
	Version string
	Comment struct {
		Value string `xml:",cdata"`
	}
	Name struct {
		Value string `xml:",cdata"`
	}
	Email string
}

// WriteXML will attempt to dump the update history to an XML file
// in order for ypkg to merge it into the package build.
func (p *PackageHistory) WriteXML(path string) error {
	var ypkgUpdates []*YPKGUpdate

	fi, err := os.Create(path)
	if err != nil {
		return err
	}
	defer fi.Close()

	for _, update := range p.Updates {
		yUpdate := &YPKGUpdate{
			Release: update.Package.Release,
			Version: update.Package.Version,
			Email:   update.AuthorEmail,
			Date:    update.Time.Format(UpdateDateFormat),
		}
		yUpdate.Comment.Value = update.Body
		yUpdate.Name.Value = update.Author
		if update.IsSecurity {
			yUpdate.Type = "security"
		}
		ypkgUpdates = append(ypkgUpdates, yUpdate)
	}

	ypkg := &YPKG{History: ypkgUpdates}
	bytes, err := xml.MarshalIndent(ypkg, "", "    ")
	if err != nil {
		return err
	}

	// Dump it to the file
	_, err = fi.WriteString(string(bytes))
	return err
}

// GetLastVersionTimestamp will return a timestamp appropriate for us within
// reproducible builds.
//
// This is calculated by using the timestamp from the last explicit version
// change, and not from simple bumps. The idea here is to only increment the
// timestamp if we've actually upgraded to a major version, and in general
// attempt to reduce the noise, and thus, produce better delta packages
// between minor package alterations
func (p *PackageHistory) GetLastVersionTimestamp() int64 {
	lastVersion := p.Updates[0].Package.Version
	lastTime := p.Updates[0].Time

	if len(p.Updates) < 2 {
		return lastTime.UTC().Unix()
	}

	// Walk history and find the last version change, assigning timestamp
	// as appropriate.
	for i := 1; i < len(p.Updates); i++ {
		newVersion := p.Updates[i].Package.Version
		if newVersion != lastVersion {
			break
		}
		lastVersion = p.Updates[i].Package.Version
		lastTime = p.Updates[i].Time
	}

	return lastTime.UTC().Unix()
}
