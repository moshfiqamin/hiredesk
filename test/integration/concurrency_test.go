//go:build integration

package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/moshfiqamin/hiredesk/internal/auth"
	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// TestConcurrentDuplicateSubmission sends the same candidate's application
// to the same post ten times at once, through the real router and a real
// database. Exactly one may be stored; the other nine must be told, with a
// 409, that the candidate has already applied.
//
// What makes this hold is the unique constraint on (job_post_id,
// candidate_id). There is no check-then-insert in the code for the requests
// to race past.
func TestConcurrentDuplicateSubmission(t *testing.T) {
	pool := startPostgres(t)
	seedSubmission(t, pool)

	tokens := auth.NewTokenManager(testJWTSecret)
	router := newRouter(t, pool, tokens)

	token, err := tokens.NewAccessToken(subCandidateID, domain.RoleCandidate)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	const attempts = 10
	const body = `{"cv_url":"https://cv.test/applicant.pdf","answers":[
		{"question_id":"` + subQWhy + `","answer":"I enjoy building reliable APIs."},
		{"question_id":"` + subQYears + `","answer":4}]}`

	// Each goroutine writes only its own element, so the slices need no lock:
	// distinct elements of a slice are distinct memory locations.
	statuses := make([]int, attempts)
	bodies := make([]string, attempts)

	// start is closed once every goroutine is waiting on it. Receiving from a
	// closed channel returns immediately in all of them at once, so the
	// requests leave together instead of in the order they were launched.
	start := make(chan struct{})

	var ready, done sync.WaitGroup
	ready.Add(attempts)
	done.Add(attempts)

	for i := range attempts {
		go func() {
			defer done.Done()

			req := httptest.NewRequest(http.MethodPost, "/v1/job-posts/"+subOpenPostID+"/applications", strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			ready.Done()
			<-start

			router.ServeHTTP(rec, req)
			statuses[i] = rec.Code
			bodies[i] = rec.Body.String()
		}()
	}

	ready.Wait()
	close(start)
	// Wait also orders memory: everything each goroutine wrote before its
	// Done is visible here after Wait returns, which is why reading the
	// slices below is not a data race.
	done.Wait()

	var created, refused int
	for i, status := range statuses {
		switch status {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
			refused++
			if got := errorCode(t, []byte(bodies[i])); got != "already_applied" {
				t.Errorf("attempt %d: 409 with code %q, want already_applied", i, got)
			}
		default:
			// A 500 here would mean the unique violation escaped untranslated.
			t.Errorf("attempt %d: status %d, body %s", i, status, bodies[i])
		}
	}
	if created != 1 || refused != attempts-1 {
		t.Errorf("created %d, refused %d; want exactly 1 and %d", created, refused, attempts-1)
	}

	// The response codes could in principle lie; the table cannot.
	var apps, answers, events int
	err = pool.QueryRow(context.Background(), `
		SELECT
			(SELECT count(*) FROM applications
			 WHERE job_post_id = $1::text::uuid AND candidate_id = $2::text::uuid),
			(SELECT count(*) FROM application_answers),
			(SELECT count(*) FROM application_events)`,
		subOpenPostID, subCandidateID,
	).Scan(&apps, &answers, &events)
	if err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if apps != 1 || answers != 2 || events != 1 {
		t.Errorf("stored %d applications, %d answers, %d events; want 1, 2, 1", apps, answers, events)
	}
}
