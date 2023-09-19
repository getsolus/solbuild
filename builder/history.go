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
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

const (
	// MaxChangelogEntries is the absolute maximum number of entries we'll
	// parse and provide changelog entries for.
	MaxChangelogEntries = 10

	// UpdateDateFormat is the time format we emit in the history.xml, i.e.
	// 2016-09-24.
	UpdateDateFormat = "2006-01-02"
)

// CveRegex is used to identify security updates which mention a specific
// CVE ID.
var CveRegex *regexp.Regexp

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
// from a git.Commit.
type PackageUpdate struct {
	Commit      plumbing.Hash // The associated git commit
	Author      string        // The author name of the change
	AuthorEmail string        // The author email of the change
	Body        string        // The associated message of the commit
	Time        time.Time     // When the update took place
	ObjectID    string        // OID stored in string form
	Package     *Package      // Associated parsed package
	IsSecurity  bool          // Whether this is a security update
}

// NewPackageUpdate will attempt to parse the given commit and provide a usable
// entry for the PackageHistory.
func NewPackageUpdate(commit *object.Commit, objectID string) *PackageUpdate {
	signature := commit.Author
	update := &PackageUpdate{
		Commit:      commit.Hash,
		Author:      signature.Name,
		AuthorEmail: signature.Email,
		Body:        commit.Message,
		Time:        signature.When,
		ObjectID:    objectID,
	}

	// Attempt to identify the update type. Limit to 1 match, we only need to
	// know IF there is a CVE fix, not how many.
	cves := CveRegex.FindAllString(update.Body, 1)
	if len(cves) > 0 {
		update.IsSecurity = true
	}

	return update
}

// CatGitBlob will return the contents of the given entry.
func CatGitBlob(repo *git.Repository, entry *object.TreeEntry) ([]byte, error) {
	obj, err := repo.BlobObject(entry.Hash)
	if err != nil {
		return nil, err
	}

	reader, err := obj.Reader()
	if err != nil {
		return nil, err
	}

	return io.ReadAll(reader)
}

// GetFileContents will attempt to read the entire object at path from
// the given tag, within that repo.
func GetFileContents(repo *git.Repository, hash plumbing.Hash, path string) ([]byte, error) {
	commit, err := repo.CommitObject(hash)
	if err != nil {
		return nil, err
	}

	tree, err := commit.Tree()
	if err != nil {
		return nil, err
	}

	entry, err := tree.FindEntry(path)
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
	// Get the root of the package repo
	pathParts := strings.Split(filepath.Dir(pkgfile), string(os.PathListSeparator))
	rootPath := filepath.Join(pathParts[:2]...)

	repo, err := git.PlainOpen(rootPath)
	if err != nil {
		return nil, err
	}

	// Get the commits for this path
	commits, err := repo.Log(&git.LogOptions{
		PathFilter: func(path string) bool {
			packageDir := filepath.Dir(pkgfile)
			return strings.HasPrefix(path, packageDir)
		},
	})

	if err != nil {
		return nil, err
	}

	var hashes []string
	updates := make(map[string]*PackageUpdate)

	// Iterate all of the tags
	err = commits.ForEach(func(commit *object.Commit) error {
		if commit == nil {
			return nil
		}

		hash := commit.ID().String()
		commitObj := NewPackageUpdate(commit, hash)
		hashes = append(hashes, hash)
		updates[hash] = commitObj

		return nil
	})

	// Foreach went bork
	if err != nil {
		return nil, err
	}

	// Sort the tags by -refname
	sort.Sort(sort.Reverse(sort.StringSlice(hashes)))

	ret := &PackageHistory{pkgfile: pkgfile}
	ret.scanUpdates(repo, updates, hashes)
	updates = nil

	if len(ret.Updates) < 1 {
		return nil, errors.New("no usable git history found")
	}

	// All done!
	return ret, nil
}

// SortUpdatesByRelease is a simple wrapper to allowing sorting history.
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
func (p *PackageHistory) scanUpdates(repo *git.Repository, updates map[string]*PackageUpdate, hashes []string) {
	// basename of file
	fname := filepath.Base(p.pkgfile)

	updateSet := make([]*PackageUpdate, 0, len(hashes))

	// Iterate the commit set in order
	for _, tagID := range hashes {
		update := updates[tagID]
		if update == nil {
			continue
		}

		b, err := GetFileContents(repo, update.Commit, fname)
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

// YPKG provides ypkg-gen-history history.xml compatibility.
type YPKG struct {
	History []*YPKGUpdate `xml:">Update"`
}

// YPKGUpdate represents an update in the package history.
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
	ypkgUpdates := make([]*YPKGUpdate, 0, len(p.Updates))

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
	_, err = fi.Write(bytes)

	return err
}

// GetLastVersionTimestamp will return a timestamp appropriate for us within
// reproducible builds.
//
// This is calculated by using the timestamp from the last explicit version
// change, and not from simple bumps. The idea here is to only increment the
// timestamp if we've actually upgraded to a major version, and in general
// attempt to reduce the noise, and thus, produce better delta packages
// between minor package alterations.
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
