package identity

import "slices"

// Permission is one thing a user may do. Handlers check permissions, never
// role names. Each constant must exist in the permissions table; a test
// enforces it.
type Permission string

const (
	PermAdminAccess   Permission = "admin.access"
	PermAnalyticsView Permission = "analytics.view"
	PermLeadsRead     Permission = "leads.read"
	PermLeadsUpdate   Permission = "leads.update"
	PermLeadsExport   Permission = "leads.export"
	PermLeadsErase    Permission = "leads.erase"
	PermUsersRead     Permission = "users.read"
	PermUsersManage   Permission = "users.manage"
	// Sprint 2: the customer-facing catalogue.
	PermCatalogRead     Permission = "catalog.read"
	PermCatalogManage   Permission = "catalog.manage"
	PermCatalogPublish  Permission = "catalog.publish"
	PermNutritionVerify Permission = "nutrition.verify"
	// Sprint 3: subscription commerce.
	PermPlansManage       Permission = "plans.manage"
	PermMenusManage       Permission = "menus.manage"
	PermMenusPublish      Permission = "menus.publish"
	PermSubscriptionsRead Permission = "subscriptions.read"
	PermOrdersRead        Permission = "orders.read"
	PermPaymentsRead      Permission = "payments.read"
	PermPaymentsRefund    Permission = "payments.refund"
	PermPromotionsManage  Permission = "promotions.manage"
	// Sprint 4: administration and CRM. users.read and users.manage are the
	// staff permissions.
	PermCustomersRead       Permission = "customers.read"
	PermCustomersSensitive  Permission = "customers.sensitive"
	PermCustomersExport     Permission = "customers.export"
	PermCRMLeadsManage      Permission = "leads.manage"
	PermCasesRead           Permission = "cases.read"
	PermCasesManage         Permission = "cases.manage"
	PermCampaignsManage     Permission = "campaigns.manage"
	PermSubscriptionsManage Permission = "subscriptions.manage"
	PermReportsRead         Permission = "reports.read"
	PermAuditRead           Permission = "audit.read"
	PermSettingsManage      Permission = "settings.manage"
	// Sprint 5: the Chef and Production ERP.
	PermIngredientsRead       Permission = "ingredients.read"
	PermIngredientsManage     Permission = "ingredients.manage"
	PermRecipesRead           Permission = "recipes.read"
	PermRecipesCreate         Permission = "recipes.create"
	PermRecipesEdit           Permission = "recipes.edit"
	PermRecipesSubmit         Permission = "recipes.submit"
	PermRecipesReview         Permission = "recipes.review"
	PermRecipesApprove        Permission = "recipes.approve"
	PermRecipesPublish        Permission = "recipes.publish"
	PermNutritionRead         Permission = "nutrition.read"
	PermNutritionReview       Permission = "nutrition.review"
	PermNutritionApprove      Permission = "nutrition.approve"
	PermChefsManage           Permission = "chefs.manage"
	PermProductionRead        Permission = "production.read"
	PermProductionPlan        Permission = "production.plan"
	PermProductionApprove     Permission = "production.approve"
	PermProductionExecute     Permission = "production.execute"
	PermProductionConfigure   Permission = "production.configure"
	PermQualityRead           Permission = "quality.read"
	PermQualityInspect        Permission = "quality.inspect"
	PermQualityReleaseHold    Permission = "quality.release_hold"
	PermPackagingManage       Permission = "packaging.manage"
	PermWasteRecord           Permission = "waste.record"
	PermProductionReportsRead Permission = "production_reports.read"
)

// AllPermissions lists every permission the code checks.
var AllPermissions = []Permission{
	PermAdminAccess, PermAnalyticsView, PermLeadsRead, PermLeadsUpdate,
	PermLeadsExport, PermLeadsErase, PermUsersRead, PermUsersManage,
	PermCatalogRead, PermCatalogManage, PermCatalogPublish, PermNutritionVerify,
	PermPlansManage, PermMenusManage, PermMenusPublish, PermSubscriptionsRead,
	PermOrdersRead, PermPaymentsRead, PermPaymentsRefund, PermPromotionsManage,
	PermCustomersRead, PermCustomersSensitive, PermCustomersExport, PermCRMLeadsManage, PermCasesRead,
	PermCasesManage, PermCampaignsManage, PermSubscriptionsManage, PermReportsRead, PermAuditRead, PermSettingsManage,
	PermIngredientsRead, PermIngredientsManage, PermRecipesRead, PermRecipesCreate, PermRecipesEdit, PermRecipesSubmit,
	PermRecipesReview, PermRecipesApprove, PermRecipesPublish, PermNutritionRead, PermNutritionReview, PermNutritionApprove,
	PermChefsManage, PermProductionRead, PermProductionPlan, PermProductionApprove, PermProductionExecute,
	PermProductionConfigure, PermQualityRead, PermQualityInspect, PermQualityReleaseHold, PermPackagingManage,
	PermWasteRecord, PermProductionReportsRead,
}

// RoleCustomer is the role every registered customer holds.
const RoleCustomer = "customer"

// Audience is the surface a session was created for.
type Audience string

const (
	AudienceCustomer Audience = "customer"
	AudienceAdmin    Audience = "admin"
)

// MFAState describes the second factor for an admin session.
type MFAState string

const (
	// MFAVerified: a code was presented in this session.
	MFAVerified MFAState = "verified"
	// MFAChallenge: the account has MFA; a code is needed before anything else.
	MFAChallenge MFAState = "challenge"
	// MFAEnrolmentRequired: MFA is required and not set up; enrol before anything else.
	MFAEnrolmentRequired MFAState = "enrolment_required"
	// MFANotEnrolled: MFA is not required here (local development) and not set up.
	MFANotEnrolled MFAState = "not_enrolled"
	// MFANotApplicable: customer sessions.
	MFANotApplicable MFAState = "not_applicable"
)

// Complete reports whether a session in this state may use the product, as
// opposed to only finishing its second-factor step.
func (s MFAState) Complete() bool {
	return s == MFAVerified || s == MFANotEnrolled || s == MFANotApplicable
}

// Principal is the authenticated caller of one request.
type Principal struct {
	UserID        string       `json:"id"`
	Email         string       `json:"email"`
	FirstName     string       `json:"first_name"`
	LastName      string       `json:"last_name"`
	EmailVerified bool         `json:"email_verified"`
	Roles         []string     `json:"roles"`
	Permissions   []Permission `json:"permissions"`
	MFA           MFAState     `json:"mfa"`

	SessionID string   `json:"-"`
	Audience  Audience `json:"-"`
}

func (p *Principal) Can(perm Permission) bool { return slices.Contains(p.Permissions, perm) }

func (p *Principal) HasRole(role string) bool { return slices.Contains(p.Roles, role) }

// Name is the display name.
func (p *Principal) Name() string {
	switch {
	case p.FirstName != "" && p.LastName != "":
		return p.FirstName + " " + p.LastName
	case p.FirstName != "":
		return p.FirstName
	}
	return p.Email
}

// staffRolePrecedence orders roles for the single "role" label the admin
// console shows; permissions always come from every role held.
var staffRolePrecedence = []string{"super_admin", "admin", "operations", "finance", "marketing", "support", "culinary_director", "production_manager", "quality_inspector", "nutritionist", "chef", "kitchen_staff", "farmer"}

// PrimaryRole is the most senior staff role held, for display only.
func (p *Principal) PrimaryRole() string {
	for _, r := range staffRolePrecedence {
		if p.HasRole(r) {
			return r
		}
	}
	if len(p.Roles) > 0 {
		return p.Roles[0]
	}
	return ""
}
