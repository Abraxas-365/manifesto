package auth

// ============================================================================
// DOMAIN-SPECIFIC SCOPES - ATS (Applicant Tracking System)
// ============================================================================

const (
	// Job scopes
	ScopeJobsAll     = "jobs:*"
	ScopeJobsRead    = "jobs:read"
	ScopeJobsWrite   = "jobs:write"
	ScopeJobsDelete  = "jobs:delete"
	ScopeJobsPublish = "jobs:publish" // Publish/unpublish jobs
	ScopeJobsArchive = "jobs:archive" // Archive jobs

	// Candidate scopes
	ScopeCandidatesAll    = "candidates:*"
	ScopeCandidatesRead   = "candidates:read"
	ScopeCandidatesWrite  = "candidates:write"
	ScopeCandidatesDelete = "candidates:delete"
	ScopeCandidatesExport = "candidates:export" // Export candidate data
	ScopeCandidatesImport = "candidates:import" // Import candidates

	// Application scopes
	ScopeApplicationsAll     = "applications:*"
	ScopeApplicationsRead    = "applications:read"
	ScopeApplicationsWrite   = "applications:write"
	ScopeApplicationsDelete  = "applications:delete"
	ScopeApplicationsReview  = "applications:review"  // Review/evaluate applications
	ScopeApplicationsApprove = "applications:approve" // Approve/reject applications
	ScopeApplicationsAssign  = "applications:assign"  // Assign to reviewers

	// Interview scopes (if you add interviews later)
	ScopeInterviewsAll      = "interviews:*"
	ScopeInterviewsRead     = "interviews:read"
	ScopeInterviewsWrite    = "interviews:write"
	ScopeInterviewsDelete   = "interviews:delete"
	ScopeInterviewsSchedule = "interviews:schedule"
	ScopeInterviewsConduct  = "interviews:conduct"

	// Offer scopes (if you add offers later)
	ScopeOffersAll     = "offers:*"
	ScopeOffersRead    = "offers:read"
	ScopeOffersWrite   = "offers:write"
	ScopeOffersDelete  = "offers:delete"
	ScopeOffersApprove = "offers:approve"
	ScopeOffersSend    = "offers:send"

	ScopeResumesAll     = "resumes:*"
	ScopeResumesRead    = "resumes:read"    // View resumes
	ScopeResumesWrite   = "resumes:write"   // Create/edit resumes
	ScopeResumesDelete  = "resumes:delete"  // Delete resumes
	ScopeResumesPublish = "resumes:publish" // Publish/activate resumes
	ScopeResumesOwn     = "resumes:own"     // Manage only own resumes (for candidates)
	ScopeResumesSearch  = "resumes:search"  // Search/semantic search resumes
	ScopeResumesExport  = "resumes:export"  // Export resume data
)

// DomainScopeCategories organizes domain-specific scopes
var DomainScopeCategories = map[string][]string{
	"Jobs": {
		ScopeJobsAll,
		ScopeJobsRead,
		ScopeJobsWrite,
		ScopeJobsDelete,
		ScopeJobsPublish,
		ScopeJobsArchive,
	},
	"Candidates": {
		ScopeCandidatesAll,
		ScopeCandidatesRead,
		ScopeCandidatesWrite,
		ScopeCandidatesDelete,
		ScopeCandidatesExport,
		ScopeCandidatesImport,
	},
	"Applications": {
		ScopeApplicationsAll,
		ScopeApplicationsRead,
		ScopeApplicationsWrite,
		ScopeApplicationsDelete,
		ScopeApplicationsReview,
		ScopeApplicationsApprove,
		ScopeApplicationsAssign,
	},
	"Interviews": {
		ScopeInterviewsAll,
		ScopeInterviewsRead,
		ScopeInterviewsWrite,
		ScopeInterviewsDelete,
		ScopeInterviewsSchedule,
		ScopeInterviewsConduct,
	},
	"Offers": {
		ScopeOffersAll,
		ScopeOffersRead,
		ScopeOffersWrite,
		ScopeOffersDelete,
		ScopeOffersApprove,
		ScopeOffersSend,
	},
	"Resumes": {
		ScopeResumesAll,
		ScopeResumesRead,
		ScopeResumesWrite,
		ScopeResumesDelete,
		ScopeResumesPublish,
		ScopeResumesOwn,
		ScopeResumesSearch,
		ScopeResumesExport,
	},
}

// DomainScopeDescriptions provides descriptions for domain scopes
var DomainScopeDescriptions = map[string]string{
	// Jobs
	ScopeJobsAll:     "Full access to job management",
	ScopeJobsRead:    "View jobs",
	ScopeJobsWrite:   "Create and edit jobs",
	ScopeJobsDelete:  "Delete jobs",
	ScopeJobsPublish: "Publish and unpublish jobs",
	ScopeJobsArchive: "Archive jobs",

	// Candidates
	ScopeCandidatesAll:    "Full access to candidate management",
	ScopeCandidatesRead:   "View candidates",
	ScopeCandidatesWrite:  "Create and edit candidates",
	ScopeCandidatesDelete: "Delete candidates",
	ScopeCandidatesExport: "Export candidate data",
	ScopeCandidatesImport: "Import candidate data",

	// Applications
	ScopeApplicationsAll:     "Full access to application management",
	ScopeApplicationsRead:    "View applications",
	ScopeApplicationsWrite:   "Create and edit applications",
	ScopeApplicationsDelete:  "Delete applications",
	ScopeApplicationsReview:  "Review and evaluate applications",
	ScopeApplicationsApprove: "Approve or reject applications",
	ScopeApplicationsAssign:  "Assign applications to reviewers",

	// Interviews
	ScopeInterviewsAll:      "Full access to interview management",
	ScopeInterviewsRead:     "View interviews",
	ScopeInterviewsWrite:    "Create and edit interviews",
	ScopeInterviewsDelete:   "Delete interviews",
	ScopeInterviewsSchedule: "Schedule interviews",
	ScopeInterviewsConduct:  "Conduct interviews",

	// Offers
	ScopeOffersAll:     "Full access to offer management",
	ScopeOffersRead:    "View offers",
	ScopeOffersWrite:   "Create and edit offers",
	ScopeOffersDelete:  "Delete offers",
	ScopeOffersApprove: "Approve offers",
	ScopeOffersSend:    "Send offers to candidates",

	ScopeResumesAll:     "Full access to resume management",
	ScopeResumesRead:    "View resumes",
	ScopeResumesWrite:   "Create and edit resumes",
	ScopeResumesDelete:  "Delete resumes",
	ScopeResumesPublish: "Publish and activate resumes",
	ScopeResumesOwn:     "Manage own resumes only (candidate access)",
	ScopeResumesSearch:  "Search resumes using semantic search",
	ScopeResumesExport:  "Export resume data",
}

// DomainScopeGroups defines domain-specific role groupings
// Update DomainScopeGroups
var DomainScopeGroups = map[string][]string{
	// Existing roles updated
	"recruiter": {
		ScopeJobsRead,
		ScopeJobsWrite,
		ScopeCandidatesAll,
		ScopeApplicationsAll,
		ScopeInterviewsAll,
		ScopeResumesAll, // Added
		ScopeReportsView,
	},
	"senior_recruiter": {
		ScopeJobsAll,
		ScopeCandidatesAll,
		ScopeApplicationsAll,
		ScopeInterviewsAll,
		ScopeResumesAll, // Added
		ScopeOffersRead,
		ScopeOffersWrite,
		ScopeReportsAll,
	},
	"hiring_manager": {
		ScopeJobsRead,
		ScopeCandidatesRead,
		ScopeApplicationsRead,
		ScopeApplicationsReview,
		ScopeApplicationsApprove,
		ScopeInterviewsRead,
		ScopeInterviewsSchedule,
		ScopeResumesRead,   // Added
		ScopeResumesSearch, // Added
		ScopeOffersRead,
		ScopeOffersApprove,
		ScopeReportsView,
	},
	"interviewer": {
		ScopeJobsRead,
		ScopeCandidatesRead,
		ScopeApplicationsRead,
		ScopeApplicationsReview,
		ScopeInterviewsRead,
		ScopeInterviewsConduct,
		ScopeResumesRead, // Added
	},

	// NEW: Candidate role
	"candidate": {
		ScopeResumesOwn,        // Can only manage their own resumes
		ScopeApplicationsWrite, // Can create applications
		ScopeApplicationsRead,  // Can view their own applications
		ScopeJobsRead,          // Can view published jobs
	},

	// NEW: Resume-specific roles
	"resume_manager": {
		ScopeResumesAll,
		ScopeCandidatesRead,
		ScopeApplicationsRead,
		ScopeJobsRead,
	},
	"resume_reviewer": {
		ScopeResumesRead,
		ScopeResumesSearch,
		ScopeCandidatesRead,
		ScopeJobsRead,
	},

	// HR roles updated
	"hr_admin": {
		ScopeJobsAll,
		ScopeCandidatesAll,
		ScopeApplicationsAll,
		ScopeInterviewsAll,
		ScopeOffersAll,
		ScopeResumesAll,
		ScopeUsersRead,
		ScopeReportsAll,
	},
}
