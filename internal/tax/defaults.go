// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package tax

// The shipped tables. Every value was entered from memory of the cited document and is to be checked against it; a user edits the stored copy, and revert restores what is here.

// VA is the name of the shipped Virginia tables, and the state a household names by default.
const VA = "VA"

var fedRates = []float64{0.10, 0.12, 0.22, 0.24, 0.32, 0.35, 0.37}

// brackets builds the seven federal brackets from the tops of the first six.
func brackets(tops ...float64) []Bracket {
	bs := []Bracket{{0, fedRates[0]}}
	for i, top := range tops {
		bs = append(bs, Bracket{top, fedRates[i+1]})
	}
	return bs
}

// fedStatuses builds the rules by filing status. The thresholds of the additional Medicare tax and the net investment income tax (26 U.S.C. 3101(b)(2), 1411) and the start of the child tax credit phase-out (26 U.S.C. 24(b)) are statutory and the same every year.
func fedStatuses(mfj, single, hoh, mfs FedStatus) map[FilingStatus]FedStatus {
	set := func(s FedStatus, phaseOut, medicare float64) FedStatus {
		s.ChildCreditPhaseOut, s.AdditionalMedicareThreshold, s.NIITThreshold = phaseOut, medicare, medicare
		return s
	}
	return map[FilingStatus]FedStatus{
		MarriedJoint:    set(mfj, 400000, 250000),
		Single:          set(single, 200000, 200000),
		HeadOfHousehold: set(hoh, 200000, 200000),
		MarriedSeparate: set(mfs, 200000, 125000),
	}
}

func fedBase(year int, status map[FilingStatus]FedStatus, child, ssBase, k401, ira, hsa float64) Federal {
	return Federal{Year: year, Status: status, ChildCredit: child, OtherDependentCredit: 500, PenaltyRate: 0.10,
		SocialSecurityRate: 0.062, SocialSecurityWageBase: ssBase, MedicareRate: 0.0145, AdditionalMedicareRate: 0.009, NIITRate: 0.038,
		Limit401k: k401, LimitIRA: ira, LimitHSA: hsa}
}

// virginia builds a Virginia table. The brackets are those of the Code of Virginia 58.1-320 and are the same for every filing status; the personal exemption is 58.1-322.03 and the 529 deduction 58.1-322.02 (up to 4,000 a year for each account).
func virginia(year int, stdSingle, stdJoint float64) State {
	return State{Name: VA, Year: year,
		Brackets:           []Bracket{{0, 0.02}, {3000, 0.03}, {5000, 0.05}, {17000, 0.0575}},
		StandardDeduction:  map[FilingStatus]float64{MarriedJoint: stdJoint, Single: stdSingle, HeadOfHousehold: stdSingle, MarriedSeparate: stdSingle},
		Exemption:          930,
		DependentExemption: 930,
		Cap529:             4000,
	}
}

// Defaults returns the shipped tables: federal and Virginia for 2024, 2025 and 2026.
func Defaults() Tables {
	return Tables{
		Federal: []Federal{
			// IRS Rev. Proc. 2023-34 (brackets, standard deduction, child tax credit 2,000 under 26 U.S.C. 24); SSA 2024 wage base announcement (168,600); IRS Notice 2023-75 (401(k) 23,000, IRA 7,000); IRS Rev. Proc. 2023-23 (HSA family 8,300).
			fedBase(2024, fedStatuses(
				FedStatus{Brackets: brackets(23200, 94300, 201050, 383900, 487450, 731200), StandardDeduction: 29200},
				FedStatus{Brackets: brackets(11600, 47150, 100525, 191950, 243725, 609350), StandardDeduction: 14600},
				FedStatus{Brackets: brackets(16550, 63100, 100500, 191950, 243700, 609350), StandardDeduction: 21900},
				FedStatus{Brackets: brackets(11600, 47150, 100525, 191950, 243725, 365600), StandardDeduction: 14600},
			), 2000, 168600, 23000, 7000, 8300),
			// IRS Rev. Proc. 2024-40 with the standard deduction and child tax credit (2,200, indexed from 2026) of the One Big Beautiful Bill Act (Pub. L. 119-21); SSA 2025 wage base announcement (176,100); IRS Notice 2024-80 (401(k) 23,500, IRA 7,000); IRS Rev. Proc. 2024-25 (HSA family 8,550).
			fedBase(2025, fedStatuses(
				FedStatus{Brackets: brackets(23850, 96950, 206700, 394600, 501050, 751600), StandardDeduction: 31500},
				FedStatus{Brackets: brackets(11925, 48475, 103350, 197300, 250525, 626350), StandardDeduction: 15750},
				FedStatus{Brackets: brackets(17000, 64850, 103350, 197300, 250500, 626350), StandardDeduction: 23625},
				FedStatus{Brackets: brackets(11925, 48475, 103350, 197300, 250525, 375800), StandardDeduction: 15750},
			), 2200, 176100, 23500, 7000, 8550),
			// IRS Rev. Proc. 2025-32 (brackets, standard deduction, child tax credit 2,200); SSA 2026 wage base announcement (184,500); IRS Notice 2025-67 (401(k) 24,500, IRA 7,500); IRS Rev. Proc. 2025-19 (HSA family 8,750).
			fedBase(2026, fedStatuses(
				FedStatus{Brackets: brackets(24800, 100800, 211400, 403550, 512450, 768700), StandardDeduction: 32200},
				FedStatus{Brackets: brackets(12400, 50400, 105700, 201775, 256225, 640600), StandardDeduction: 16100},
				FedStatus{Brackets: brackets(17700, 67450, 105700, 201750, 256200, 640600), StandardDeduction: 24150},
				FedStatus{Brackets: brackets(12400, 50400, 105700, 201775, 256225, 384350), StandardDeduction: 16100},
			), 2200, 184500, 24500, 7500, 8750),
		},
		State: []State{
			// Virginia Tax instructions for Form 760 (2024 and 2025: standard deduction 8,500 single and 17,000 married filing jointly).
			virginia(2024, 8500, 17000),
			virginia(2025, 8500, 17000),
			// Tax year 2026 is carried over from 2025 until Virginia Tax publishes its instructions.
			virginia(2026, 8500, 17000),
		},
	}
}
