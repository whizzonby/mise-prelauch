package leads

import (
	"net/mail"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/referrals"
)

var (
	// Names are letters with ordinary name punctuation. This is deliberately
	// strict: the name is echoed in emails we send, and a free-text field would
	// let someone use our mail server to deliver their own message or link.
	namePattern = regexp.MustCompile(`^[\p{L}\p{M}][\p{L}\p{M}' .\-]*$`)
	// Slugs identify options defined by the content configuration (locations,
	// dietary interests, meal categories).
	slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
	idPattern   = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)
)

const (
	maxListItems  = 12
	minFillMillis = 1500
)

type fieldErrors []httpx.FieldError

func (f *fieldErrors) add(field, code, message string) {
	*f = append(*f, httpx.FieldError{Field: field, Code: code, Message: message})
}

// normalizedSignup is a SignupInput that has passed validation.
type normalizedSignup struct {
	FirstName      string
	Email          string
	EmailCanonical string
	Phone          *string
	Location       string
	Dietary        []string
	HouseholdSize  *int
	Packaging      *string
	ReferralCode   string
	Attribution    Attribution
	AnonymousID    string
}

func validateSignup(in SignupInput) (normalizedSignup, error) {
	var errs fieldErrors
	out := normalizedSignup{}

	out.FirstName = strings.Join(strings.Fields(in.FirstName), " ")
	switch {
	case out.FirstName == "":
		errs.add("first_name", "required", "Enter your first name.")
	case utf8.RuneCountInString(out.FirstName) > 80:
		errs.add("first_name", "too_long", "Use 80 characters or fewer.")
	case !namePattern.MatchString(out.FirstName):
		errs.add("first_name", "invalid", "Use letters only, with spaces, hyphens or apostrophes.")
	}

	email, canonical, ok := NormalizeEmail(in.Email)
	switch {
	case strings.TrimSpace(in.Email) == "":
		errs.add("email", "required", "Enter your email address.")
	case !ok:
		errs.add("email", "invalid", "Enter a valid email address, like name@example.com.")
	}
	out.Email, out.EmailCanonical = email, canonical

	if strings.TrimSpace(in.Phone) != "" {
		phone, ok := NormalizePhone(in.Phone)
		if !ok {
			errs.add("phone", "invalid", "Enter a phone number with 7 to 15 digits, or leave it blank.")
		} else {
			out.Phone = &phone
		}
	}

	out.Location = strings.TrimSpace(in.Location)
	switch {
	case out.Location == "":
		errs.add("location", "required", "Choose where you would like delivery.")
	case !slugPattern.MatchString(out.Location):
		errs.add("location", "invalid", "Choose a location from the list.")
	}

	dietary, ok := normalizeSlugs(in.DietaryInterests)
	if !ok {
		errs.add("dietary_interests", "invalid", "Choose dietary interests from the list.")
	}
	out.Dietary = dietary

	if in.HouseholdSize != nil {
		if *in.HouseholdSize < 1 || *in.HouseholdSize > 12 {
			errs.add("household_size", "out_of_range", "Household size must be between 1 and 12.")
		} else {
			out.HouseholdSize = in.HouseholdSize
		}
	}

	if p := strings.TrimSpace(in.PackagingPreference); p != "" {
		if !slugPattern.MatchString(p) {
			errs.add("packaging_preference", "invalid", "Choose a packaging option from the list.")
		} else {
			out.Packaging = &p
		}
	}

	if !in.Consent {
		errs.add("consent", "required", "Tick the box so we can email you about the launch.")
	}

	// An unusable referral code never blocks a signup; it is simply ignored.
	if code := referrals.NormalizeCode(in.ReferralCode); referrals.ValidCode(code) {
		out.ReferralCode = code
	}
	if idPattern.MatchString(in.AnonymousID) {
		out.AnonymousID = in.AnonymousID
	}
	if in.Attribution != nil {
		out.Attribution = Attribution{First: cleanTouch(in.Attribution.First), Latest: cleanTouch(in.Attribution.Latest)}
	}

	if len(errs) > 0 {
		return out, httpx.ValidationError(errs)
	}
	return out, nil
}

// looksAutomated applies the two cheap bot checks: a filled honeypot, or a form
// submitted faster than a person can type an email address.
func looksAutomated(in SignupInput) bool {
	return strings.TrimSpace(in.Website) != "" || in.ElapsedMS < minFillMillis
}

// NormalizeEmail returns the deliverable form (trimmed, lower-cased) and the
// canonical form used for uniqueness.
func NormalizeEmail(raw string) (email, canonical string, ok bool) {
	email = strings.ToLower(strings.TrimSpace(raw))
	if email == "" || len(email) > 254 {
		return "", "", false
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return "", "", false
	}
	at := strings.LastIndex(email, "@")
	local, domain := email[:at], email[at+1:]
	if len(local) > 64 || !strings.Contains(domain, ".") || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") || strings.Contains(domain, "..") {
		return "", "", false
	}
	for _, r := range email {
		if r > unicode.MaxASCII || unicode.IsSpace(r) || unicode.IsControl(r) {
			return "", "", false
		}
	}

	canonLocal, _, _ := strings.Cut(local, "+")
	canonDomain := domain
	if domain == "gmail.com" || domain == "googlemail.com" {
		canonLocal = strings.ReplaceAll(canonLocal, ".", "")
		canonDomain = "gmail.com"
	}
	if canonLocal == "" {
		return "", "", false
	}
	return email, canonLocal + "@" + canonDomain, true
}

// NormalizePhone keeps digits and a leading plus sign.
func NormalizePhone(raw string) (string, bool) {
	var b strings.Builder
	for i, r := range strings.TrimSpace(raw) {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '+' && i == 0:
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '(' || r == ')' || r == '.':
		default:
			return "", false
		}
	}
	phone := b.String()
	digits := len(strings.TrimPrefix(phone, "+"))
	if digits < 7 || digits > 15 {
		return "", false
	}
	return phone, true
}

func normalizeSlugs(in []string) ([]string, bool) {
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if !slugPattern.MatchString(s) {
			return nil, false
		}
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	if len(out) > maxListItems {
		return nil, false
	}
	return out, true
}

func cleanTouch(t *Touch) *Touch {
	if t == nil {
		return nil
	}
	c := Touch{
		UTMSource:   cleanText(t.UTMSource, 120),
		UTMMedium:   cleanText(t.UTMMedium, 120),
		UTMCampaign: cleanText(t.UTMCampaign, 120),
		UTMContent:  cleanText(t.UTMContent, 120),
		UTMTerm:     cleanText(t.UTMTerm, 120),
		LandingPage: cleanText(t.LandingPage, 300),
		ReferrerURL: cleanText(t.ReferrerURL, 300),
	}
	if c.empty() {
		return nil
	}
	return &c
}

// cleanText trims, drops control characters and truncates. Attribution is
// best-effort marketing data, so it is cleaned rather than rejected.
func cleanText(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.TrimSpace(s))
	if utf8.RuneCountInString(s) > max {
		s = string([]rune(s)[:max])
	}
	return s
}

// normalizedPreferences holds validated profiling answers. Nil means "leave
// the stored value unchanged".
type normalizedPreferences struct {
	HouseholdSize    *int
	MealsPerWeek     *int
	Dietary          *[]string
	MealInterests    *[]string
	CookingFrequency *string
	DeliveryArea     *string
	Packaging        *string
	Metadata         map[string]string
}

func validatePreferences(in PreferencesInput) (normalizedPreferences, error) {
	var errs fieldErrors
	out := normalizedPreferences{Metadata: map[string]string{}}

	if in.HouseholdSize != nil {
		if *in.HouseholdSize < 1 || *in.HouseholdSize > 12 {
			errs.add("household_size", "out_of_range", "Household size must be between 1 and 12.")
		}
		out.HouseholdSize = in.HouseholdSize
	}
	if in.MealsPerWeek != nil {
		if *in.MealsPerWeek < 1 || *in.MealsPerWeek > 21 {
			errs.add("meals_per_week", "out_of_range", "Meals per week must be between 1 and 21.")
		}
		out.MealsPerWeek = in.MealsPerWeek
	}
	slugList := func(field string, in *[]string) *[]string {
		if in == nil {
			return nil
		}
		list, ok := normalizeSlugs(*in)
		if !ok {
			errs.add(field, "invalid", "Choose options from the list.")
			return nil
		}
		return &list
	}
	out.Dietary = slugList("dietary_preferences", in.DietaryPreferences)
	out.MealInterests = slugList("meal_interests", in.MealInterests)

	slug := func(field string, in *string) *string {
		if in == nil {
			return nil
		}
		v := strings.TrimSpace(*in)
		if !slugPattern.MatchString(v) {
			errs.add(field, "invalid", "Choose an option from the list.")
			return nil
		}
		return &v
	}
	out.CookingFrequency = slug("cooking_frequency", in.CookingFrequency)
	out.DeliveryArea = slug("delivery_area", in.DeliveryArea)
	out.Packaging = slug("packaging_preference", in.PackagingPreference)
	for key, v := range map[string]*string{
		"household_type": in.HouseholdType,
		"fitness_goal":   in.FitnessGoal,
		"usage":          in.Usage,
	} {
		if s := slug(key, v); s != nil {
			out.Metadata[key] = *s
		}
	}

	if len(errs) > 0 {
		return out, httpx.ValidationError(errs)
	}
	return out, nil
}
