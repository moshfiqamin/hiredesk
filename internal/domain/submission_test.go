package domain

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// The form every submission test is checked against: one question of each
// kind, two required and two optional.
const (
	qWhy    = "q-why"    // text, required, min_length 20
	qYears  = "q-years"  // number, required
	qRemote = "q-remote" // boolean, optional
	qCity   = "q-city"   // choice, optional
)

func testForm() []Question {
	return []Question{
		{ID: qWhy, Kind: QuestionKindText, Required: true, MinLength: 20, Position: 1},
		{ID: qYears, Kind: QuestionKindNumber, Required: true, Position: 2},
		{ID: qRemote, Kind: QuestionKindBoolean, Position: 3},
		{ID: qCity, Kind: QuestionKindChoice, Choices: []string{"Dhaka", "Remote"}, Position: 4},
	}
}

// validSubmission answers every question correctly. Each test case changes
// one thing about it.
func validSubmission() NewApplication {
	return NewApplication{
		JobPostID:   "post-1",
		CandidateID: "candidate-1",
		CVURL:       "https://cv.example/me.pdf",
		Answers: []NewAnswer{
			{QuestionID: qWhy, Answer: "I have built three hiring tools in Go."},
			{QuestionID: qYears, Answer: "4"},
			{QuestionID: qRemote, Answer: "true"},
			{QuestionID: qCity, Answer: "Dhaka"},
		},
	}
}

// answer replaces the answer to one question, or adds it if absent.
func answer(in *NewApplication, questionID, text string) {
	for i := range in.Answers {
		if in.Answers[i].QuestionID == questionID {
			in.Answers[i].Answer = text
			return
		}
	}
	in.Answers = append(in.Answers, NewAnswer{QuestionID: questionID, Answer: text})
}

// without removes the answer to one question.
func without(in *NewApplication, questionID string) {
	kept := in.Answers[:0]
	for _, a := range in.Answers {
		if a.QuestionID != questionID {
			kept = append(kept, a)
		}
	}
	in.Answers = kept
}

// problems runs the validation and returns each answer problem's code, in
// order, and the cv_url problem. It fails the test if err is neither nil nor
// a *SubmissionError.
func problems(t *testing.T, err error) (codes []string, cvURL string) {
	t.Helper()

	if err == nil {
		return nil, ""
	}
	var invalid *SubmissionError
	if !errors.As(err, &invalid) {
		t.Fatalf("error = %v (%T), want a *SubmissionError", err, err)
	}
	if !errors.Is(err, ErrValidation) {
		t.Errorf("errors.Is(err, ErrValidation) = false")
	}
	for _, p := range invalid.Answers {
		codes = append(codes, p.Code)
	}
	return codes, invalid.CVURL
}

// submissionCase is one row of a rule's table.
type submissionCase struct {
	name      string
	edit      func(in *NewApplication)
	wantCodes []string // answer problem codes, in order; nil means none
	wantCV    bool     // whether cv_url must have a problem
}

func runSubmissionCases(t *testing.T, tests []submissionCase) {
	t.Helper()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			in := validSubmission()
			tt.edit(&in)

			_, err := ValidateApplication(testForm(), in)
			codes, cv := problems(t, err)

			if !reflect.DeepEqual(codes, tt.wantCodes) {
				t.Errorf("codes = %v, want %v", codes, tt.wantCodes)
			}
			if (cv != "") != tt.wantCV {
				t.Errorf("cv_url problem = %q, want one: %v", cv, tt.wantCV)
			}
		})
	}
}

// Rule 1.
func TestCheckAcceptingApplications(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status        string
		wantNil       bool
		wantNotFound  bool
		wantNotAccept bool
	}{
		{JobStatusOpen, true, false, false},
		{JobStatusDraft, false, true, false},
		{JobStatusClosed, false, false, true},
		{"archived", false, false, true},
		{"", false, false, true},
	}

	for _, tt := range tests {
		t.Run("status "+tt.status, func(t *testing.T) {
			t.Parallel()

			err := CheckAcceptingApplications(tt.status)
			if (err == nil) != tt.wantNil {
				t.Fatalf("err = %v, want nil: %v", err, tt.wantNil)
			}
			if got := errors.Is(err, ErrNotFound); got != tt.wantNotFound {
				t.Errorf("errors.Is(err, ErrNotFound) = %v, want %v", got, tt.wantNotFound)
			}

			var notAccepting *NotAcceptingError
			if got := errors.As(err, &notAccepting); got != tt.wantNotAccept {
				t.Fatalf("errors.As(err, *NotAcceptingError) = %v, want %v", got, tt.wantNotAccept)
			}
			if tt.wantNotAccept {
				if !errors.Is(err, ErrNotAcceptingApplications) {
					t.Errorf("does not match ErrNotAcceptingApplications")
				}
				if got := notAccepting.Details()["current_status"]; got != tt.status {
					t.Errorf("details current_status = %v, want %q", got, tt.status)
				}
			}
		})
	}
}

// Rule 2.
func TestValidateApplicationRequired(t *testing.T) {
	t.Parallel()

	runSubmissionCases(t, []submissionCase{
		{"every question answered", func(*NewApplication) {}, nil, false},
		{"only the required questions answered", func(in *NewApplication) {
			without(in, qRemote)
			without(in, qCity)
		}, nil, false},
		{"an optional answer left blank", func(in *NewApplication) { answer(in, qCity, "   ") }, nil, false},

		{"a required question not answered", func(in *NewApplication) { without(in, qYears) },
			[]string{AnswerRequired}, false},
		{"a required answer that is empty", func(in *NewApplication) { answer(in, qWhy, "") },
			[]string{AnswerRequired}, false},
		{"a required answer that is only spaces", func(in *NewApplication) { answer(in, qWhy, " \t\n ") },
			[]string{AnswerRequired}, false},
		{"no answers at all", func(in *NewApplication) { in.Answers = nil },
			[]string{AnswerRequired, AnswerRequired}, false},
	})
}

// Rule 3.
func TestValidateApplicationUnknownQuestion(t *testing.T) {
	t.Parallel()

	runSubmissionCases(t, []submissionCase{
		{"a question from another job post", func(in *NewApplication) { answer(in, "q-from-another-post", "hello") },
			[]string{AnswerUnknownQuestion}, false},
		{"a question ID that is not a UUID", func(in *NewApplication) { answer(in, "'; DROP TABLE questions; --", "x") },
			[]string{AnswerUnknownQuestion}, false},
		{"an empty question ID", func(in *NewApplication) { answer(in, "", "x") },
			[]string{AnswerUnknownQuestion}, false},
		{"one question answered twice", func(in *NewApplication) {
			in.Answers = append(in.Answers, NewAnswer{QuestionID: qYears, Answer: "5"})
		}, []string{AnswerDuplicate}, false},

		// The unknown answer cannot stand in for the missing required one.
		{"an unknown answer in place of a required one", func(in *NewApplication) {
			without(in, qYears)
			answer(in, "q-from-another-post", "4")
		}, []string{AnswerRequired, AnswerUnknownQuestion}, false},
	})
}

// Rule 4.
func TestValidateApplicationKind(t *testing.T) {
	t.Parallel()

	runSubmissionCases(t, []submissionCase{
		{"a whole number", func(in *NewApplication) { answer(in, qYears, "12") }, nil, false},
		{"a negative decimal", func(in *NewApplication) { answer(in, qYears, "-3.5") }, nil, false},
		{"exponent notation", func(in *NewApplication) { answer(in, qYears, "1e3") }, nil, false},
		{"a number with spaces around it", func(in *NewApplication) { answer(in, qYears, " 7 ") }, nil, false},
		{"a word for a number", func(in *NewApplication) { answer(in, qYears, "four") }, []string{AnswerInvalidNumber}, false},
		{"a number with a unit", func(in *NewApplication) { answer(in, qYears, "4 years") }, []string{AnswerInvalidNumber}, false},
		{"NaN", func(in *NewApplication) { answer(in, qYears, "NaN") }, []string{AnswerInvalidNumber}, false},
		{"Inf", func(in *NewApplication) { answer(in, qYears, "Inf") }, []string{AnswerInvalidNumber}, false},
		{"a number too large for float64", func(in *NewApplication) { answer(in, qYears, "1e400") }, []string{AnswerInvalidNumber}, false},

		{"false", func(in *NewApplication) { answer(in, qRemote, "false") }, nil, false},
		{"TRUE in capitals", func(in *NewApplication) { answer(in, qRemote, "TRUE") }, nil, false},
		{"yes for a boolean", func(in *NewApplication) { answer(in, qRemote, "yes") }, []string{AnswerInvalidBoolean}, false},
		{"1 for a boolean", func(in *NewApplication) { answer(in, qRemote, "1") }, []string{AnswerInvalidBoolean}, false},
		{"t for a boolean", func(in *NewApplication) { answer(in, qRemote, "t") }, []string{AnswerInvalidBoolean}, false},

		{"the other choice", func(in *NewApplication) { answer(in, qCity, "Remote") }, nil, false},
		{"a choice in another case", func(in *NewApplication) { answer(in, qCity, "dhaka") }, nil, false},
		{"a choice not offered", func(in *NewApplication) { answer(in, qCity, "Chittagong") }, []string{AnswerInvalidChoice}, false},
		{"a prefix of a choice", func(in *NewApplication) { answer(in, qCity, "Dha") }, []string{AnswerInvalidChoice}, false},
	})
}

// Rule 5.
func TestValidateApplicationMinLength(t *testing.T) {
	t.Parallel()

	runSubmissionCases(t, []submissionCase{
		{"exactly min_length", func(in *NewApplication) { answer(in, qWhy, strings.Repeat("a", 20)) }, nil, false},
		{"min_length in multi-byte characters", func(in *NewApplication) { answer(in, qWhy, strings.Repeat("é", 20)) }, nil, false},
		{"one short", func(in *NewApplication) { answer(in, qWhy, strings.Repeat("a", 19)) },
			[]string{AnswerTooShort}, false},
		{"padding does not count", func(in *NewApplication) { answer(in, qWhy, "   short answer      ") },
			[]string{AnswerTooShort}, false},
	})

	t.Run("reports min_length and got", func(t *testing.T) {
		t.Parallel()

		in := validSubmission()
		answer(&in, qWhy, "  Because.  ")
		_, err := ValidateApplication(testForm(), in)

		var invalid *SubmissionError
		if !errors.As(err, &invalid) || len(invalid.Answers) != 1 {
			t.Fatalf("err = %v, want one problem", err)
		}
		got := invalid.Details()["answers"].([]map[string]any)[0]
		want := map[string]any{
			"question_id": qWhy, "index": 0, "code": AnswerTooShort,
			"message": "must be at least 20 characters", "min_length": 20, "got": 8,
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("details = %v\n     want %v", got, want)
		}
	})
}

// Rule 6.
func TestValidateApplicationCVURL(t *testing.T) {
	t.Parallel()

	cv := func(url string) func(*NewApplication) {
		return func(in *NewApplication) { in.CVURL = url }
	}

	runSubmissionCases(t, []submissionCase{
		{"https", cv("https://cv.example/me.pdf"), nil, false},
		{"http", cv("http://cv.example/me.pdf"), nil, false},
		{"a scheme in capitals", cv("HTTPS://cv.example/me.pdf"), nil, false},
		{"a port and a query", cv("https://files.example:8443/cv?id=7"), nil, false},
		{"spaces around it", cv("  https://cv.example/me.pdf  "), nil, false},
		{"exactly the length limit", cv("https://cv.example/" + strings.Repeat("a", MaxCVURLLength-len("https://cv.example/"))), nil, false},

		{"missing", cv(""), nil, true},
		{"only spaces", cv("   "), nil, true},
		{"a bare file name", cv("cv.pdf"), nil, true},
		{"a path with no host", cv("/files/cv.pdf"), nil, true},
		{"no scheme", cv("cv.example/me.pdf"), nil, true},
		{"a scheme-relative URL", cv("//cv.example/me.pdf"), nil, true},
		{"a scheme and nothing else", cv("https://"), nil, true},
		{"ftp", cv("ftp://cv.example/me.pdf"), nil, true},
		{"javascript", cv("javascript:alert(1)"), nil, true},
		{"mailto", cv("mailto:me@example.com"), nil, true},
		{"a space in the host", cv("https://cv example.com/me.pdf"), nil, true},
		{"over the length limit", cv("https://cv.example/" + strings.Repeat("a", MaxCVURLLength)), nil, true},
	})
}

// The point of collecting: one request that breaks every rule gets every
// problem back, in rule order, in one response.
func TestValidateApplicationCollectsEveryProblem(t *testing.T) {
	t.Parallel()

	in := NewApplication{
		CVURL: "cv.pdf",
		Answers: []NewAnswer{
			{QuestionID: qWhy, Answer: "Too short."},      // 0: rule 5
			{QuestionID: qCity, Answer: "Mars"},           // 1: rule 4
			{QuestionID: "q-elsewhere", Answer: "x"},      // 2: rule 3
			{QuestionID: qRemote, Answer: "maybe"},        // 3: rule 4
			{QuestionID: qCity, Answer: "Dhaka"},          // 4: rule 3, a second answer
			{QuestionID: "q-also-elsewhere", Answer: "y"}, // 5: rule 3
			// qYears is required and unanswered:             rule 2
		},
	}

	_, err := ValidateApplication(testForm(), in)

	var invalid *SubmissionError
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want a *SubmissionError", err)
	}

	type got struct {
		code     string
		question string
		index    int
	}
	var all []got
	for _, p := range invalid.Answers {
		all = append(all, got{p.Code, p.QuestionID, p.Index})
	}
	want := []got{
		{AnswerRequired, qYears, NoIndex},
		{AnswerUnknownQuestion, "q-elsewhere", 2},
		{AnswerDuplicate, qCity, 4},
		{AnswerUnknownQuestion, "q-also-elsewhere", 5},
		{AnswerInvalidChoice, qCity, 1},
		{AnswerInvalidBoolean, qRemote, 3},
		{AnswerTooShort, qWhy, 0},
	}
	if !reflect.DeepEqual(all, want) {
		t.Errorf("problems =\n %v\nwant\n %v", all, want)
	}
	if invalid.CVURL == "" {
		t.Errorf("cv_url problem missing")
	}

	details := invalid.Details()
	if _, ok := details["cv_url"]; !ok {
		t.Errorf("details has no cv_url: %v", details)
	}
	first := details["answers"].([]map[string]any)[0]
	if _, hasIndex := first["index"]; hasIndex {
		t.Errorf("a missing answer has an index: %v", first)
	}
	choice := details["answers"].([]map[string]any)[4]
	if !reflect.DeepEqual(choice["choices"], []string{"Dhaka", "Remote"}) {
		t.Errorf("invalid choice lists %v, want the form's options", choice["choices"])
	}

	// Neither the CV link nor any answer may reach a log line.
	if msg := err.Error(); strings.Contains(msg, "cv.pdf") || strings.Contains(msg, "Mars") {
		t.Errorf("Error() leaks input: %q", msg)
	}
}

// What a valid submission returns is what the store writes.
func TestValidateApplicationCanonicalForm(t *testing.T) {
	t.Parallel()

	in := NewApplication{
		JobPostID:   "post-1",
		CandidateID: "candidate-1",
		CVURL:       "  https://cv.example/me.pdf ",
		Answers: []NewAnswer{
			{QuestionID: qWhy, Answer: "  I have built three hiring tools in Go.  "},
			{QuestionID: qYears, Answer: " 4 "},
			{QuestionID: qRemote, Answer: "FALSE"},
			{QuestionID: qCity, Answer: "remote"},
		},
	}

	got, err := ValidateApplication(testForm(), in)
	if err != nil {
		t.Fatalf("err = %v", err)
	}

	want := NewApplication{
		JobPostID:   "post-1",
		CandidateID: "candidate-1",
		CVURL:       "https://cv.example/me.pdf",
		Answers: []NewAnswer{
			{QuestionID: qWhy, Answer: "I have built three hiring tools in Go."},
			{QuestionID: qYears, Answer: "4"},
			{QuestionID: qRemote, Answer: "false"},
			{QuestionID: qCity, Answer: "Remote"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}

	t.Run("a blank optional answer is dropped", func(t *testing.T) {
		t.Parallel()

		in := validSubmission()
		answer(&in, qCity, "")
		got, err := ValidateApplication(testForm(), in)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		for _, a := range got.Answers {
			if a.QuestionID == qCity {
				t.Errorf("blank answer kept: %+v", a)
			}
		}
		if len(got.Answers) != 3 {
			t.Errorf("answers = %d, want 3", len(got.Answers))
		}
	})

	t.Run("the caller's slice is not modified", func(t *testing.T) {
		t.Parallel()

		in := validSubmission()
		answer(&in, qRemote, "TRUE")
		if _, err := ValidateApplication(testForm(), in); err != nil {
			t.Fatalf("err = %v", err)
		}
		for _, a := range in.Answers {
			if a.QuestionID == qRemote && a.Answer != "TRUE" {
				t.Errorf("input changed to %q", a.Answer)
			}
		}
	})
}
