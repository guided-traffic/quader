package v1

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/malware"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/signatures"
)

// The security dashboard's view of signatures (signatures.md R-SIG-038, R-SIG-034): invalid signatures and the signatures of
// blocked signers with the files that hold them. The scope is malware's (R-MAL-022): a holder of content:metadata-read reads every
// repository in full; a caller with read on some repositories sees the files there, and an entry only with them.

// signatureFindingsShown is the most files and repositories one entry lists, as malware's finds do (Q-MAL-45).
const signatureFindingsShown = 100

func signatureFindingsSpec() ListSpec {
	return ListSpec{Filters: []string{"kind", "repository", "q"}, Sorts: []string{"foundAt"}, DefaultSort: "-foundAt"}
}

// ListSignatureFindings implements GET /api/v1/signatures/findings.
func (s *Server) ListSignatureFindings(w http.ResponseWriter, r *http.Request, _ gen.ListSignatureFindingsParams) {
	sv, err := s.signatureServices()
	var lq ListQuery
	if err == nil {
		lq, err = ParseList(r, signatureFindingsSpec(), s.o.API.Config)
	}
	var fq signatures.FindingsQuery
	if err == nil {
		fq, err = s.signatureFindingsQuery(r.Context(), sv, lq)
	}
	var page Page[gen.SignatureFinding]
	if err == nil {
		page, err = s.signatureFindingsPage(r.Context(), sv, lq, fq)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	WritePage(w, r, page)
}

// signatureFindingsQuery reads the filters and the cursor (the found time and the signature's ID of the last entry read).
func (s *Server) signatureFindingsQuery(ctx context.Context, sv SignatureServices, lq ListQuery) (signatures.FindingsQuery, error) {
	fq := signatures.FindingsQuery{Limit: lq.FetchLimit(), Q: lq.First("q")}
	for _, v := range splitValues(lq.Filter["kind"]) {
		if v != signatures.FindingInvalid && v != signatures.FindingBlocked {
			return fq, badParam("kind", "must be invalid or blocked")
		}
		if !slices.Contains(fq.Kinds, v) {
			fq.Kinds = append(fq.Kinds, v)
		}
	}
	if utf8.RuneCountInString(fq.Q) > 256 {
		return fq, badParam("q", "must be at most 256 characters")
	}
	if name := lq.First("repository"); name != "" {
		key, err := qname.ParseRepo(name)
		if err != nil {
			return fq, badParam("repository", "must be a repository name <project>/<key>")
		}
		if sv.Repos == nil {
			return fq, unavailable("The repository snapshot")
		}
		repo, err := sv.Repos.ByName(ctx, key)
		if err != nil {
			if errs.KindOf(err) == errs.NotFound {
				return fq, badParam("repository", "names no repository")
			}
			return fq, err
		}
		fq.RepoID = repo.NumericID()
	}
	if lq.After != nil {
		if len(lq.After) != 2 {
			return fq, invalidCursor()
		}
		at, err := time.Parse(time.RFC3339Nano, lq.After[0])
		id, ierr := strconv.ParseInt(lq.After[1], 10, 64)
		if err != nil || ierr != nil {
			return fq, invalidCursor()
		}
		fq.AfterAt, fq.AfterID = at, id
	}
	return fq, nil
}

func (s *Server) signatureFindingsPage(ctx context.Context, sv SignatureServices, lq ListQuery, fq signatures.FindingsQuery) (Page[gen.SignatureFinding], error) {
	holders, err := sv.Store.FindingHolders(ctx, fq.Kinds)
	if err != nil {
		return Page[gen.SignatureFinding]{}, err
	}
	ctx, sc, err := s.malwareScopeOf(ctx, MalwareServices{Decider: sv.Decider}, holderRepos(holders))
	if err != nil {
		return Page[gen.SignatureFinding]{}, err
	}
	fq.RepoIDs = append([]int64{}, sc.ids...)
	finds, err := sv.Store.Findings(ctx, fq)
	if err != nil {
		return Page[gen.SignatureFinding]{}, err
	}
	// The cursor is the last entry the store read, whether or not the caller sees one of its files: a page of hidden entries must
	// not end the list.
	var next *string
	if len(finds) > lq.Limit {
		finds = finds[:lq.Limit]
		last := finds[len(finds)-1]
		c := lq.EncodeCursor([]string{last.FoundAt.UTC().Format(time.RFC3339Nano), strconv.FormatInt(last.SignatureID, 10)})
		next = &c
	}
	out := Page[gen.SignatureFinding]{Items: []gen.SignatureFinding{}, NextCursor: next}
	for _, f := range finds {
		seen, err := sc.visible(ctx, holderItems(f.Holders, sc))
		if err != nil {
			return Page[gen.SignatureFinding]{}, err
		}
		if len(seen) == 0 {
			continue
		}
		out.Items = append(out.Items, s.signatureFindingRep(ctx, f, seen))
	}
	return out, nil
}

// signatureFindingRep renders one entry with the files the caller may see, each with the package and version its format gives the
// path (the files classifier, as the decision uses it).
func (s *Server) signatureFindingRep(ctx context.Context, f signatures.Finding, seen []malware.Item) gen.SignatureFinding {
	var who signatures.Signer
	if len(f.Signer) > 0 {
		_ = json.Unmarshal(f.Signer, &who)
	}
	out := gen.SignatureFinding{
		Id: f.SignatureID, Kind: gen.SignatureFindingKind(f.Kind), Scheme: gen.SignatureScheme(f.Scheme), Role: gen.SignatureRole(f.Role),
		Reason: optionalText(f.Reason), Signer: optionalText(who.Subject), Identity: optionalText(who.Identity), Issuer: optionalText(who.Issuer),
		KeyId: optionalText(who.KeyID), Subject: hex.EncodeToString(f.Subject[:]), SubjectName: optionalText(f.SubjectName),
		FoundAt: f.FoundAt, Block: sigBlockRef(f.Block), Items: []gen.SignatureFindingItem{}, Repositories: []string{},
	}
	if out.Scheme == "" {
		out.Scheme = gen.SignatureScheme(signatures.SchemeUnknown)
	}
	for _, it := range seen {
		name := it.ProjectKey + "/" + it.RepoKey
		if !slices.Contains(out.Repositories, name) {
			out.Repositories = append(out.Repositories, name)
		}
		if len(out.Items) >= signatureFindingsShown {
			continue
		}
		item := gen.SignatureFindingItem{Repository: name, Format: it.Format, Path: optionalText(it.Path)}
		if it.Path != "" && s.o.Files != nil {
			c := s.o.Files.Classified(ctx, authz.Coordinate{Repository: qname.Repo{Project: it.ProjectKey, Key: it.RepoKey}, Path: it.Path})
			item.Package, item.Version = optionalText(c.Package), optionalText(c.Version)
		}
		out.Items = append(out.Items, item)
	}
	slices.Sort(out.Repositories)
	out.RepositoryCount = len(out.Repositories)
	out.FirstRepository = out.Repositories[0]
	if len(out.Repositories) > signatureFindingsShown {
		out.Repositories = out.Repositories[:signatureFindingsShown]
	}
	out.ItemsTruncated = len(seen) > signatureFindingsShown
	return out
}
