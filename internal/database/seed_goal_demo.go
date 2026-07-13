package database

import (
	"log"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// seedGoalManagementDemoData populates a small, realistic, idempotent demo
// dataset spanning Master Data, Goal Management, and KPI Management so a
// fresh GOAL_MANAGEMENT=true environment isn't empty. Safe to call on every
// startup — skips entirely if the marker pillar already exists, so it never
// creates duplicates or clobbers real user-entered data.
//
// Lineage demonstrated per goal:
//
//	Goal -> Parent Objective (OperationalObjective, w/ Pillar+Enabler)
//	     -> Child Objective (Process, w/ Pillar+Enabler)
//	     -> Strategic KPI (via ProcessID) + Operational KPI (via ProcessID)
//	     -> Initiative (w/ Parent Objective, Pillar, Enabler)
func seedGoalManagementDemoData(db *gorm.DB, ownerID uuid.UUID) {
	const marker = "Governance Excellence"
	var existing models.Pillar
	if db.Where("name_en = ?", marker).First(&existing).Error != gorm.ErrRecordNotFound {
		return // already seeded
	}

	log.Println("Seeding Goal/KPI Management demo data...")

	// ── Pillars ──────────────────────────────────────────────────────────
	pillarGovernance := &models.Pillar{NameEn: "Governance Excellence", OwnerID: &ownerID}
	pillarDigital := &models.Pillar{NameEn: "Digital Transformation", OwnerID: &ownerID}
	pillarCommunity := &models.Pillar{NameEn: "Community Wellbeing", OwnerID: &ownerID}
	db.Create(pillarGovernance)
	db.Create(pillarDigital)
	db.Create(pillarCommunity)

	// ── Enablers ─────────────────────────────────────────────────────────
	enablerTech := &models.Enabler{NameEn: "Technology & Innovation", OwnerID: &ownerID}
	enablerHuman := &models.Enabler{NameEn: "Human Capital", OwnerID: &ownerID}
	enablerPartners := &models.Enabler{NameEn: "Strategic Partnerships", OwnerID: &ownerID}
	db.Create(enablerTech)
	db.Create(enablerHuman)
	db.Create(enablerPartners)

	// ── Domains ──────────────────────────────────────────────────────────
	db.Create(&models.Domain{NameEn: "Strategic Planning", Type: models.DomainTypeStrategy})
	db.Create(&models.Domain{NameEn: "Excellence Award", Type: models.DomainTypeAward})

	// ── Award Criteria + Sub-Criteria ───────────────────────────────────
	critLeadership := &models.AwardCriterion{CriterionNo: 1, NameEn: "Leadership & Governance"}
	db.Create(critLeadership)
	subStrategicDirection := &models.AwardSubCriterion{AwardCriterionID: critLeadership.ID, SubNo: "1.1", NameEn: "Strategic Direction"}
	db.Create(subStrategicDirection)
	db.Create(&models.AwardSubCriterion{AwardCriterionID: critLeadership.ID, SubNo: "1.2", NameEn: "Governance Structure"})

	critCustomer := &models.AwardCriterion{CriterionNo: 2, NameEn: "Customer Focus"}
	db.Create(critCustomer)
	db.Create(&models.AwardSubCriterion{AwardCriterionID: critCustomer.ID, SubNo: "2.1", NameEn: "Customer Engagement"})
	db.Create(&models.AwardSubCriterion{AwardCriterionID: critCustomer.ID, SubNo: "2.2", NameEn: "Service Quality"})

	// ── Goals ────────────────────────────────────────────────────────────
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	target := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	review := time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)

	goalDigital := &models.Goal{
		Title:       "Enhance Digital Government Services",
		Description: "Modernize citizen-facing digital platforms to improve accessibility, speed, and satisfaction.",
		Priority:    "High",
		OwnerID:     ownerID,
		CreatedByID: ownerID,
		StartDate:   &start,
		TargetDate:  &target,
		ReviewDate:  &review,
		Metadata:    "{}",
	}
	db.Create(goalDigital)
	db.Model(goalDigital).Update("path", goalDigital.ID.String())

	goalInfra := &models.Goal{
		Title:       "Improve Municipal Infrastructure Quality",
		Description: "Upgrade road networks and utility infrastructure to meet growing service demand.",
		Priority:    "Critical",
		OwnerID:     ownerID,
		CreatedByID: ownerID,
		StartDate:   &start,
		TargetDate:  &target,
		ReviewDate:  &review,
		Metadata:    "{}",
	}
	db.Create(goalInfra)
	db.Model(goalInfra).Update("path", goalInfra.ID.String())

	goalCommunity := &models.Goal{
		Title:       "Strengthen Community Engagement",
		Description: "Expand channels for citizen participation in local decision-making.",
		Priority:    "Medium",
		OwnerID:     ownerID,
		CreatedByID: ownerID,
		StartDate:   &start,
		TargetDate:  &target,
		ReviewDate:  &review,
		Metadata:    "{}",
	}
	db.Create(goalCommunity)
	db.Model(goalCommunity).Update("path", goalCommunity.ID.String())

	// ── Parent Objectives (OperationalObjective) ────────────────────────
	poDigital := &models.OperationalObjective{
		NameEn: "Modernize Digital Platforms", GoalID: &goalDigital.ID,
		PillarID: &pillarDigital.ID, EnablerID: &enablerTech.ID,
	}
	db.Create(poDigital)

	poInfra := &models.OperationalObjective{
		NameEn: "Upgrade Road & Utility Networks", GoalID: &goalInfra.ID,
		PillarID: &pillarGovernance.ID, EnablerID: &enablerPartners.ID,
	}
	db.Create(poInfra)

	poCommunity := &models.OperationalObjective{
		NameEn: "Expand Citizen Participation Channels", GoalID: &goalCommunity.ID,
		PillarID: &pillarCommunity.ID, EnablerID: &enablerHuman.ID,
	}
	db.Create(poCommunity)

	// ── Child Objectives (Process) ──────────────────────────────────────
	coDigital := &models.Process{
		NameEn: "Digital Service Delivery", OperationalObjectiveID: poDigital.ID,
		GoalID: &goalDigital.ID, PillarID: &pillarDigital.ID, EnablerID: &enablerTech.ID,
		Unit: "Digital Services Department",
	}
	db.Create(coDigital)

	coInfra := &models.Process{
		NameEn: "Road Maintenance Operations", OperationalObjectiveID: poInfra.ID,
		GoalID: &goalInfra.ID, PillarID: &pillarGovernance.ID, EnablerID: &enablerPartners.ID,
		Unit: "Public Works Department",
	}
	db.Create(coInfra)

	coCommunity := &models.Process{
		NameEn: "Community Outreach Programs", OperationalObjectiveID: poCommunity.ID,
		GoalID: &goalCommunity.ID, PillarID: &pillarCommunity.ID, EnablerID: &enablerHuman.ID,
		Unit: "Community Affairs Department",
	}
	db.Create(coCommunity)

	// ── Strategic KPIs (Goal -> Objective) ──────────────────────────────
	skDigital := &models.StrategicKPI{
		Code: "KPI-STR-001", NameEn: "Digital Service Adoption Rate",
		GoalID: &goalDigital.ID, ProcessID: &coDigital.ID, PillarID: &pillarDigital.ID,
		Polarity: models.KPIPolarityAscending, ActivationStatus: models.KPIStatusActive,
		Baseline: 45, UnitOfMeasure: "%", ReportingFrequency: models.KPIFrequencyQuarterly,
		DescriptionEn: "Share of eligible citizen transactions completed via digital channels.",
	}
	db.Create(skDigital)
	db.Create(&models.KpiAnnualTarget{KpiCode: skDigital.Code, KpiType: models.KPITypeStrategic, Year: 2026, PeriodType: "annual", PeriodKey: "2026", TargetValue: 80})

	skInfra := &models.StrategicKPI{
		Code: "KPI-STR-002", NameEn: "Road Network Quality Index",
		GoalID: &goalInfra.ID, ProcessID: &coInfra.ID, PillarID: &pillarGovernance.ID,
		Polarity: models.KPIPolarityAscending, ActivationStatus: models.KPIStatusActive,
		Baseline: 60, UnitOfMeasure: "index", ReportingFrequency: models.KPIFrequencyAnnually,
		DescriptionEn: "Composite index of pavement condition across the road network.",
	}
	db.Create(skInfra)
	db.Create(&models.KpiAnnualTarget{KpiCode: skInfra.Code, KpiType: models.KPITypeStrategic, Year: 2026, PeriodType: "annual", PeriodKey: "2026", TargetValue: 85})

	skCommunity := &models.StrategicKPI{
		Code: "KPI-STR-003", NameEn: "Citizen Satisfaction Score",
		GoalID: &goalCommunity.ID, ProcessID: &coCommunity.ID, PillarID: &pillarCommunity.ID,
		Polarity: models.KPIPolarityAscending, ActivationStatus: models.KPIStatusActive,
		Baseline: 70, UnitOfMeasure: "%", ReportingFrequency: models.KPIFrequencyQuarterly,
		DescriptionEn: "Overall citizen satisfaction with municipal engagement channels.",
	}
	db.Create(skCommunity)
	db.Create(&models.KpiAnnualTarget{KpiCode: skCommunity.Code, KpiType: models.KPITypeStrategic, Year: 2026, PeriodType: "annual", PeriodKey: "2026", TargetValue: 90})

	// ── Operational KPIs (Goal -> Parent Objective -> Child Objective) ──
	okDigital := &models.OperationalKPI{
		Code: "KPI-OPS-001", NameEn: "Average Service Request Turnaround",
		GoalID: &goalDigital.ID, OperationalObjectiveID: poDigital.ID, ProcessID: coDigital.ID,
		Polarity: models.KPIPolarityDescending, ActivationStatus: models.KPIStatusActive,
		Baseline: 5, UnitOfMeasure: "days", ReportingFrequency: models.KPIFrequencyMonthly,
		DescriptionEn: "Average time to resolve a digital service request end-to-end.",
	}
	db.Create(okDigital)
	db.Create(&models.KpiAnnualTarget{KpiCode: okDigital.Code, KpiType: models.KPITypeOperational, Year: 2026, PeriodType: "annual", PeriodKey: "2026", TargetValue: 2})

	okInfra := &models.OperationalKPI{
		Code: "KPI-OPS-002", NameEn: "Pothole Repair Response Time",
		GoalID: &goalInfra.ID, OperationalObjectiveID: poInfra.ID, ProcessID: coInfra.ID,
		Polarity: models.KPIPolarityDescending, ActivationStatus: models.KPIStatusActive,
		Baseline: 3, UnitOfMeasure: "days", ReportingFrequency: models.KPIFrequencyMonthly,
		DescriptionEn: "Average time from pothole report to repair completion.",
	}
	db.Create(okInfra)
	db.Create(&models.KpiAnnualTarget{KpiCode: okInfra.Code, KpiType: models.KPITypeOperational, Year: 2026, PeriodType: "annual", PeriodKey: "2026", TargetValue: 1})

	okCommunity := &models.OperationalKPI{
		Code: "KPI-OPS-003", NameEn: "Community Event Attendance",
		GoalID: &goalCommunity.ID, OperationalObjectiveID: poCommunity.ID, ProcessID: coCommunity.ID,
		Polarity: models.KPIPolarityAscending, ActivationStatus: models.KPIStatusActive,
		Baseline: 200, UnitOfMeasure: "attendees", ReportingFrequency: models.KPIFrequencyQuarterly,
		DescriptionEn: "Average attendance across community outreach events.",
	}
	db.Create(okCommunity)
	db.Create(&models.KpiAnnualTarget{KpiCode: okCommunity.Code, KpiType: models.KPITypeOperational, Year: 2026, PeriodType: "annual", PeriodKey: "2026", TargetValue: 350})

	// ── Award KPI (Award Criteria/Sub-Criteria lineage) ─────────────────
	db.Create(&models.AwardKPI{
		Code: "KPI-AW-001", NameEn: "Governance Maturity Score",
		AwardSubCriterionID: subStrategicDirection.ID,
		Polarity:            models.KPIPolarityAscending, ActivationStatus: models.KPIStatusActive,
		Baseline: 3, UnitOfMeasure: "score (1-5)", ReportingFrequency: models.KPIFrequencyAnnually,
		DescriptionEn: "Self-assessed maturity of strategic governance practices.",
	})

	// ── Initiatives (Goal + Parent Objective) ───────────────────────────
	db.Create(&models.Initiative{
		NameEn: "Citizen Portal Revamp Initiative", GoalID: &goalDigital.ID, ObjectiveID: &poDigital.ID,
		PillarID: &pillarDigital.ID, EnablerID: &enablerTech.ID, OwnerID: &ownerID,
		Status: models.InitiativeStatusActive,
	})
	db.Create(&models.Initiative{
		NameEn: "Road Resurfacing Program 2026", GoalID: &goalInfra.ID, ObjectiveID: &poInfra.ID,
		PillarID: &pillarGovernance.ID, EnablerID: &enablerPartners.ID, OwnerID: &ownerID,
		Status: models.InitiativeStatusActive,
	})
	db.Create(&models.Initiative{
		NameEn: "Neighborhood Councils Rollout", GoalID: &goalCommunity.ID, ObjectiveID: &poCommunity.ID,
		PillarID: &pillarCommunity.ID, EnablerID: &enablerHuman.ID, OwnerID: &ownerID,
		Status: models.InitiativeStatusDraft,
	})

	log.Println("Goal/KPI Management demo data seeded: 3 pillars, 3 enablers, 2 domains, 2 award criteria, 3 goals, 3 parent objectives, 3 child objectives, 3 strategic KPIs, 3 operational KPIs, 1 award KPI, 3 initiatives.")
}
