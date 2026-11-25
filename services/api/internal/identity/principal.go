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

	// Sprint 6: suppliers, procurement, purchasing, receiving and inventory.
	PermSuppliersRead              Permission = "suppliers.read"
	PermSuppliersManage            Permission = "suppliers.manage"
	PermSuppliersApprove           Permission = "suppliers.approve"
	PermSupplierProductsRead       Permission = "supplier_products.read"
	PermSupplierProductsManage     Permission = "supplier_products.manage"
	PermProcurementRead            Permission = "procurement.read"
	PermProcurementPlan            Permission = "procurement.plan"
	PermProcurementApprove         Permission = "procurement.approve"
	PermProcurementConfigure       Permission = "procurement.configure"
	PermPurchaseOrdersRead         Permission = "purchase_orders.read"
	PermPurchaseOrdersCreate       Permission = "purchase_orders.create"
	PermPurchaseOrdersApprove      Permission = "purchase_orders.approve"
	PermPurchaseOrdersApproveLimit Permission = "purchase_orders.approve_above_limit"
	PermPurchaseOrdersIssue        Permission = "purchase_orders.issue"
	PermReceivingRead              Permission = "receiving.read"
	PermReceivingManage            Permission = "receiving.manage"
	PermReceivingInspect           Permission = "receiving.inspect"
	PermInventoryRead              Permission = "inventory.read"
	PermInventoryReserve           Permission = "inventory.reserve"
	PermInventoryIssue             Permission = "inventory.issue"
	PermInventoryAdjust            Permission = "inventory.adjust"
	PermInventoryCount             Permission = "inventory.count"
	PermInventoryConfigure         Permission = "inventory.configure"
	PermTraceabilityRead           Permission = "traceability.read"
	PermSupplierReportsRead        Permission = "supplier_reports.read"

	// Sprint 7: fulfillment and delivery.
	PermFulfillmentRead          Permission = "fulfillment.read"
	PermFulfillmentVerify        Permission = "fulfillment.verify"
	PermFulfillmentRelease       Permission = "fulfillment.release"
	PermDeliveryRead             Permission = "delivery.read"
	PermDeliveryReadAssigned     Permission = "delivery.read_assigned"
	PermDeliveryPlan             Permission = "delivery.plan"
	PermDeliveryAssign           Permission = "delivery.assign"
	PermDeliveryDispatch         Permission = "delivery.dispatch"
	PermDeliveryUpdateStatus     Permission = "delivery.update_status"
	PermDeliveryReschedule       Permission = "delivery.reschedule"
	PermDeliveryConfirm          Permission = "delivery.confirm"
	PermDeliveryReconcile        Permission = "delivery.reconcile"
	PermDeliveryPartnersRead     Permission = "delivery_partners.read"
	PermDeliveryPartnersManage   Permission = "delivery_partners.manage"
	PermDeliveryPartnersApprove  Permission = "delivery_partners.approve"
	PermDeliveryZonesRead        Permission = "delivery_zones.read"
	PermDeliveryZonesManage      Permission = "delivery_zones.manage"
	PermDeliveryProofRead        Permission = "delivery_proof.read"
	PermDeliveryProofCreate      Permission = "delivery_proof.create"
	PermDeliveryExceptionsRead   Permission = "delivery_exceptions.read"
	PermDeliveryExceptionsManage Permission = "delivery_exceptions.manage"
	PermDeliveryReportsRead      Permission = "delivery_reports.read"
	PermDeliverySettingsManage   Permission = "delivery_settings.manage"

	// Sprint 8: analytics, automation and forecasting.
	PermAnalyticsExecutiveRead   Permission = "analytics_executive.read"
	PermAnalyticsRevenueRead     Permission = "analytics_revenue.read"
	PermAnalyticsCustomersRead   Permission = "analytics_customers.read"
	PermAnalyticsProductionRead  Permission = "analytics_production.read"
	PermAnalyticsProcurementRead Permission = "analytics_procurement.read"
	PermAnalyticsInventoryRead   Permission = "analytics_inventory.read"
	PermAnalyticsDeliveryRead    Permission = "analytics_delivery.read"
	PermAnalyticsExport          Permission = "analytics.export"
	PermAutomationRead           Permission = "automation.read"
	PermAutomationManage         Permission = "automation.manage"
	PermAutomationExecute        Permission = "automation.execute"
	PermAutomationApprove        Permission = "automation.approve"
	PermForecastingRead          Permission = "forecasting.read"
	PermForecastingRun           Permission = "forecasting.run"
	PermForecastingConfigure     Permission = "forecasting.configure"

	// Sprint 9: launch readiness.
	PermLaunchRead    Permission = "launch.read"
	PermLaunchSignoff Permission = "launch.signoff"

	// Sprint 9.1: payment providers.
	PermPaymentProvidersRead     Permission = "payment_providers.read"
	PermPaymentProvidersManage   Permission = "payment_providers.manage"
	PermPaymentProvidersActivate Permission = "payment_providers.activate"

	// Sprint 9.1: Mise Chat and the help centre.
	PermSupportChatRead          Permission = "support_chat.read"
	PermSupportChatReply         Permission = "support_chat.reply"
	PermSupportChatAssign        Permission = "support_chat.assign"
	PermSupportChatTransfer      Permission = "support_chat.transfer"
	PermSupportChatResolve       Permission = "support_chat.resolve"
	PermSupportChatInternalNotes Permission = "support_chat.internal_notes"
	PermSupportChatManage        Permission = "support_chat.manage"
	PermSupportChatAnalytics     Permission = "support_chat.analytics"
	PermHelpCentreEdit           Permission = "help_centre.edit"
	PermHelpCentrePublish        Permission = "help_centre.publish"
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
	PermSuppliersRead, PermSuppliersManage, PermSuppliersApprove, PermSupplierProductsRead, PermSupplierProductsManage,
	PermProcurementRead, PermProcurementPlan, PermProcurementApprove, PermProcurementConfigure, PermPurchaseOrdersRead,
	PermPurchaseOrdersCreate, PermPurchaseOrdersApprove, PermPurchaseOrdersApproveLimit, PermPurchaseOrdersIssue,
	PermReceivingRead, PermReceivingManage, PermReceivingInspect, PermInventoryRead, PermInventoryReserve, PermInventoryIssue,
	PermInventoryAdjust, PermInventoryCount, PermInventoryConfigure, PermTraceabilityRead, PermSupplierReportsRead,
	PermFulfillmentRead, PermFulfillmentVerify, PermFulfillmentRelease, PermDeliveryRead, PermDeliveryReadAssigned, PermDeliveryPlan,
	PermDeliveryAssign, PermDeliveryDispatch, PermDeliveryUpdateStatus, PermDeliveryReschedule, PermDeliveryConfirm, PermDeliveryReconcile,
	PermDeliveryPartnersRead, PermDeliveryPartnersManage, PermDeliveryPartnersApprove, PermDeliveryZonesRead, PermDeliveryZonesManage,
	PermDeliveryProofRead, PermDeliveryProofCreate, PermDeliveryExceptionsRead, PermDeliveryExceptionsManage, PermDeliveryReportsRead,
	PermDeliverySettingsManage,
	PermAnalyticsExecutiveRead, PermAnalyticsRevenueRead, PermAnalyticsCustomersRead, PermAnalyticsProductionRead, PermAnalyticsProcurementRead,
	PermAnalyticsInventoryRead, PermAnalyticsDeliveryRead, PermAnalyticsExport, PermAutomationRead, PermAutomationManage, PermAutomationExecute,
	PermAutomationApprove, PermForecastingRead, PermForecastingRun, PermForecastingConfigure,
	PermLaunchRead, PermLaunchSignoff,
	PermPaymentProvidersRead, PermPaymentProvidersManage, PermPaymentProvidersActivate,
	PermSupportChatRead, PermSupportChatReply, PermSupportChatAssign, PermSupportChatTransfer, PermSupportChatResolve,
	PermSupportChatInternalNotes, PermSupportChatManage, PermSupportChatAnalytics, PermHelpCentreEdit, PermHelpCentrePublish,
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
