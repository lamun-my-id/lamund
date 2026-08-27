package api

import (
	"net/http"
	"sort"
	"strings"

	"github.com/lamun-my-id/lamund/internal/store"
)

// handleSearchUsers: pencarian user untuk invite tim ala GitHub. Butuh query
// minimal 4 karakter; hasil di-rank (exact > prefix > substring > typo≤2),
// exact selalu di atas. Balikin username+name saja (tanpa data sensitif).
func (s *server) handleSearchUsers(w http.ResponseWriter, r *http.Request) {
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	if len([]rune(q)) < 4 {
		writeJSON(w, http.StatusOK, map[string]any{"users": []store.UserLite{}})
		return
	}
	cands, err := s.d.Store.SearchUserCandidates(q, 50)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal mencari pengguna")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": rankUserMatches(cands, q, 8)})
}

type scoredUser struct {
	u    store.UserLite
	rank int
	tie  int
}

// rankUserMatches memberi peringkat kandidat terhadap query:
//
//	0 = username sama persis (selalu paling atas)
//	1 = username diawali query
//	2 = username mengandung query
//	3 = username mirip (Levenshtein ≤ 2 — toleransi typo 1-2 huruf)
//	4 = name mengandung query
//
// Kandidat di luar itu dibuang. Kembalikan maksimal `limit` teratas.
func rankUserMatches(cands []store.UserLite, q string, limit int) []store.UserLite {
	scored := make([]scoredUser, 0, len(cands))
	for _, c := range cands {
		u := strings.ToLower(c.Username)
		n := strings.ToLower(c.Name)
		var rank, tie int
		switch {
		case u == q:
			rank, tie = 0, 0
		case strings.HasPrefix(u, q):
			rank, tie = 1, len(u)
		case strings.Contains(u, q):
			rank, tie = 2, strings.Index(u, q)
		default:
			if d := levenshtein(u, q); d <= 2 {
				rank, tie = 3, d
			} else if strings.Contains(n, q) {
				rank, tie = 4, 0
			} else {
				continue
			}
		}
		scored = append(scored, scoredUser{c, rank, tie})
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].rank != scored[j].rank {
			return scored[i].rank < scored[j].rank
		}
		if scored[i].tie != scored[j].tie {
			return scored[i].tie < scored[j].tie
		}
		return scored[i].u.Username < scored[j].u.Username
	})
	if len(scored) > limit {
		scored = scored[:limit]
	}
	out := make([]store.UserLite, len(scored))
	for i, s := range scored {
		out[i] = s.u
	}
	return out
}

// levenshtein menghitung edit distance dua string (rune-aware) dengan DP baris
// tunggal. Dipakai untuk toleransi typo pada pencarian user.
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	if len(ra) == 0 {
		return len(rb)
	}
	if len(rb) == 0 {
		return len(ra)
	}
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min3(cur[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}
