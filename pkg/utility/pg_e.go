package utility

import (
	"fmt"
	"slices"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

// pgeNBC defines the total non-bypassable charges for PG&E.
// Public Purpose Program ($0.00614) + Nuclear Decommissioning (-$0.00002) +
// Competition Transition Charge ($0.00027) + Wildfire Fund ($0.00591) = $0.01230.
const pgeNBC = 0.01230

// getPGENBTExportRate returns the export rate for PG&E Net Billing Tariff (NBT)
// modeled using the real 2026 credit values in pgeNBTData (produced + delivered).
// https://www.pge.com/en/clean-energy/solar/getting-started-with-solar/solar-billing-plan.html
func getPGENBTExportRate(t time.Time) float64 {
	year := t.Year()
	monthStr := t.Month().String()[:3]
	dayType := "Weekday"
	if t.Weekday() == time.Saturday || t.Weekday() == time.Sunday || slices.Contains(getPGEHolidays(year), t.Format("2006-01-02")) {
		dayType = "Weekend"
	}
	key := fmt.Sprintf("%s_%s_%d", monthStr, dayType, t.Hour())
	if yearMap, ok := pgeNBTData[year]; ok {
		if val, ok := yearMap[key]; ok {
			return val
		}
	}
	// Fallback to 2026 if not found
	if val, ok := pgeNBTData[2026][key]; ok {
		return val
	}
	return 0.05 // safe fallback
}

// pgeNBTData contains PG&E SBP (NBT) export rates ($/kWh) for 2026,
// representing the sum of Produced and Delivered credits, grouped by Month_DayType_Hour.
var pgeNBTData = map[int]map[string]float64{
	2026: {
		"Jan_Weekday_0":  0.09431,
		"Jan_Weekday_1":  0.09245,
		"Jan_Weekday_2":  0.09089,
		"Jan_Weekday_3":  0.08983,
		"Jan_Weekday_4":  0.09182,
		"Jan_Weekday_5":  0.09405,
		"Jan_Weekday_6":  0.09492,
		"Jan_Weekday_7":  0.08954,
		"Jan_Weekday_8":  0.07747,
		"Jan_Weekday_9":  0.06926,
		"Jan_Weekday_10": 0.06863,
		"Jan_Weekday_11": 0.06848,
		"Jan_Weekday_12": 0.06721,
		"Jan_Weekday_13": 0.06555,
		"Jan_Weekday_14": 0.06411,
		"Jan_Weekday_15": 0.06654,
		"Jan_Weekday_16": 0.08244,
		"Jan_Weekday_17": 0.10945,
		"Jan_Weekday_18": 0.10249,
		"Jan_Weekday_19": 0.09429,
		"Jan_Weekday_20": 0.09352,
		"Jan_Weekday_21": 0.09452,
		"Jan_Weekday_22": 0.09741,
		"Jan_Weekday_23": 0.09587,
		"Jan_Weekend_0":  0.09113,
		"Jan_Weekend_1":  0.09067,
		"Jan_Weekend_2":  0.09184,
		"Jan_Weekend_3":  0.09067,
		"Jan_Weekend_4":  0.09020,
		"Jan_Weekend_5":  0.09153,
		"Jan_Weekend_6":  0.09349,
		"Jan_Weekend_7":  0.08473,
		"Jan_Weekend_8":  0.06956,
		"Jan_Weekend_9":  0.05979,
		"Jan_Weekend_10": 0.05861,
		"Jan_Weekend_11": 0.05798,
		"Jan_Weekend_12": 0.05858,
		"Jan_Weekend_13": 0.05755,
		"Jan_Weekend_14": 0.05629,
		"Jan_Weekend_15": 0.05862,
		"Jan_Weekend_16": 0.08529,
		"Jan_Weekend_17": 0.10588,
		"Jan_Weekend_18": 0.09963,
		"Jan_Weekend_19": 0.09280,
		"Jan_Weekend_20": 0.09103,
		"Jan_Weekend_21": 0.09138,
		"Jan_Weekend_22": 0.09432,
		"Jan_Weekend_23": 0.09361,
		"Feb_Weekday_0":  0.09397,
		"Feb_Weekday_1":  0.09236,
		"Feb_Weekday_2":  0.09344,
		"Feb_Weekday_3":  0.09370,
		"Feb_Weekday_4":  0.09454,
		"Feb_Weekday_5":  0.09227,
		"Feb_Weekday_6":  0.08892,
		"Feb_Weekday_7":  0.08405,
		"Feb_Weekday_8":  0.06463,
		"Feb_Weekday_9":  0.05398,
		"Feb_Weekday_10": 0.05185,
		"Feb_Weekday_11": 0.05081,
		"Feb_Weekday_12": 0.04672,
		"Feb_Weekday_13": 0.04259,
		"Feb_Weekday_14": 0.03482,
		"Feb_Weekday_15": 0.03470,
		"Feb_Weekday_16": 0.06764,
		"Feb_Weekday_17": 0.10769,
		"Feb_Weekday_18": 0.09537,
		"Feb_Weekday_19": 0.08910,
		"Feb_Weekday_20": 0.08822,
		"Feb_Weekday_21": 0.08936,
		"Feb_Weekday_22": 0.09413,
		"Feb_Weekday_23": 0.09141,
		"Feb_Weekend_0":  0.08539,
		"Feb_Weekend_1":  0.08766,
		"Feb_Weekend_2":  0.09041,
		"Feb_Weekend_3":  0.09211,
		"Feb_Weekend_4":  0.08939,
		"Feb_Weekend_5":  0.08656,
		"Feb_Weekend_6":  0.08151,
		"Feb_Weekend_7":  0.07389,
		"Feb_Weekend_8":  0.05012,
		"Feb_Weekend_9":  0.02912,
		"Feb_Weekend_10": 0.03136,
		"Feb_Weekend_11": 0.02697,
		"Feb_Weekend_12": 0.02884,
		"Feb_Weekend_13": 0.02660,
		"Feb_Weekend_14": 0.03051,
		"Feb_Weekend_15": 0.02368,
		"Feb_Weekend_16": 0.06153,
		"Feb_Weekend_17": 0.10028,
		"Feb_Weekend_18": 0.10371,
		"Feb_Weekend_19": 0.09029,
		"Feb_Weekend_20": 0.08752,
		"Feb_Weekend_21": 0.08744,
		"Feb_Weekend_22": 0.08976,
		"Feb_Weekend_23": 0.09348,
		"Mar_Weekday_0":  0.08125,
		"Mar_Weekday_1":  0.07819,
		"Mar_Weekday_2":  0.07470,
		"Mar_Weekday_3":  0.07439,
		"Mar_Weekday_4":  0.07590,
		"Mar_Weekday_5":  0.07899,
		"Mar_Weekday_6":  0.07983,
		"Mar_Weekday_7":  0.07309,
		"Mar_Weekday_8":  0.05282,
		"Mar_Weekday_9":  0.02635,
		"Mar_Weekday_10": 0.02205,
		"Mar_Weekday_11": 0.02009,
		"Mar_Weekday_12": 0.01926,
		"Mar_Weekday_13": 0.01742,
		"Mar_Weekday_14": 0.01689,
		"Mar_Weekday_15": 0.01753,
		"Mar_Weekday_16": 0.02556,
		"Mar_Weekday_17": 0.06213,
		"Mar_Weekday_18": 0.08653,
		"Mar_Weekday_19": 0.08464,
		"Mar_Weekday_20": 0.08030,
		"Mar_Weekday_21": 0.07667,
		"Mar_Weekday_22": 0.07604,
		"Mar_Weekday_23": 0.07740,
		"Mar_Weekend_0":  0.06798,
		"Mar_Weekend_1":  0.06912,
		"Mar_Weekend_2":  0.06839,
		"Mar_Weekend_3":  0.07309,
		"Mar_Weekend_4":  0.07366,
		"Mar_Weekend_5":  0.07571,
		"Mar_Weekend_6":  0.07432,
		"Mar_Weekend_7":  0.06766,
		"Mar_Weekend_8":  0.00999,
		"Mar_Weekend_9":  0.00387,
		"Mar_Weekend_10": 0.00479,
		"Mar_Weekend_11": 0.00681,
		"Mar_Weekend_12": 0.00936,
		"Mar_Weekend_13": 0.00158,
		"Mar_Weekend_14": 0.00080,
		"Mar_Weekend_15": 0.00721,
		"Mar_Weekend_16": 0.01894,
		"Mar_Weekend_17": 0.03422,
		"Mar_Weekend_18": 0.07581,
		"Mar_Weekend_19": 0.07708,
		"Mar_Weekend_20": 0.07480,
		"Mar_Weekend_21": 0.07270,
		"Mar_Weekend_22": 0.07367,
		"Mar_Weekend_23": 0.07040,
		"Apr_Weekday_0":  0.07779,
		"Apr_Weekday_1":  0.07675,
		"Apr_Weekday_2":  0.08053,
		"Apr_Weekday_3":  0.07941,
		"Apr_Weekday_4":  0.07759,
		"Apr_Weekday_5":  0.07751,
		"Apr_Weekday_6":  0.07117,
		"Apr_Weekday_7":  0.06147,
		"Apr_Weekday_8":  0.01732,
		"Apr_Weekday_9":  0.00711,
		"Apr_Weekday_10": 0.00838,
		"Apr_Weekday_11": 0.00839,
		"Apr_Weekday_12": 0.00850,
		"Apr_Weekday_13": 0.00391,
		"Apr_Weekday_14": 0.00180,
		"Apr_Weekday_15": 0.00018,
		"Apr_Weekday_16": 0.00141,
		"Apr_Weekday_17": 0.00697,
		"Apr_Weekday_18": 0.07828,
		"Apr_Weekday_19": 0.07830,
		"Apr_Weekday_20": 0.07418,
		"Apr_Weekday_21": 0.07264,
		"Apr_Weekday_22": 0.07191,
		"Apr_Weekday_23": 0.07640,
		"Apr_Weekend_0":  0.06613,
		"Apr_Weekend_1":  0.06757,
		"Apr_Weekend_2":  0.07222,
		"Apr_Weekend_3":  0.07676,
		"Apr_Weekend_4":  0.07514,
		"Apr_Weekend_5":  0.07546,
		"Apr_Weekend_6":  0.06859,
		"Apr_Weekend_7":  0.04780,
		"Apr_Weekend_8":  0.00488,
		"Apr_Weekend_9":  0.00599,
		"Apr_Weekend_10": 0.00684,
		"Apr_Weekend_11": 0.01103,
		"Apr_Weekend_12": 0.00926,
		"Apr_Weekend_13": 0.00305,
		"Apr_Weekend_14": 0.00002,
		"Apr_Weekend_15": 0.00002,
		"Apr_Weekend_16": 0.00002,
		"Apr_Weekend_17": 0.00650,
		"Apr_Weekend_18": 0.07318,
		"Apr_Weekend_19": 0.08148,
		"Apr_Weekend_20": 0.07011,
		"Apr_Weekend_21": 0.06936,
		"Apr_Weekend_22": 0.06811,
		"Apr_Weekend_23": 0.07126,
		"May_Weekday_0":  0.07821,
		"May_Weekday_1":  0.07432,
		"May_Weekday_2":  0.07655,
		"May_Weekday_3":  0.07989,
		"May_Weekday_4":  0.08127,
		"May_Weekday_5":  0.07577,
		"May_Weekday_6":  0.06987,
		"May_Weekday_7":  0.06423,
		"May_Weekday_8":  0.02562,
		"May_Weekday_9":  0.01380,
		"May_Weekday_10": 0.01454,
		"May_Weekday_11": 0.01249,
		"May_Weekday_12": 0.01330,
		"May_Weekday_13": 0.01588,
		"May_Weekday_14": 0.01242,
		"May_Weekday_15": 0.01225,
		"May_Weekday_16": 0.01506,
		"May_Weekday_17": 0.03458,
		"May_Weekday_18": 0.08162,
		"May_Weekday_19": 0.08734,
		"May_Weekday_20": 0.07612,
		"May_Weekday_21": 0.07213,
		"May_Weekday_22": 0.07302,
		"May_Weekday_23": 0.07522,
		"May_Weekend_0":  0.06790,
		"May_Weekend_1":  0.07168,
		"May_Weekend_2":  0.07841,
		"May_Weekend_3":  0.07984,
		"May_Weekend_4":  0.07667,
		"May_Weekend_5":  0.06817,
		"May_Weekend_6":  0.06977,
		"May_Weekend_7":  0.03635,
		"May_Weekend_8":  0.00324,
		"May_Weekend_9":  0.00630,
		"May_Weekend_10": 0.00560,
		"May_Weekend_11": 0.00595,
		"May_Weekend_12": 0.00835,
		"May_Weekend_13": 0.00817,
		"May_Weekend_14": 0.00230,
		"May_Weekend_15": 0.00001,
		"May_Weekend_16": 0.00341,
		"May_Weekend_17": 0.00523,
		"May_Weekend_18": 0.06212,
		"May_Weekend_19": 0.08498,
		"May_Weekend_20": 0.07422,
		"May_Weekend_21": 0.07263,
		"May_Weekend_22": 0.07136,
		"May_Weekend_23": 0.07264,
		"Jun_Weekday_0":  0.07839,
		"Jun_Weekday_1":  0.07696,
		"Jun_Weekday_2":  0.07558,
		"Jun_Weekday_3":  0.07777,
		"Jun_Weekday_4":  0.07599,
		"Jun_Weekday_5":  0.07598,
		"Jun_Weekday_6":  0.07301,
		"Jun_Weekday_7":  0.06660,
		"Jun_Weekday_8":  0.04699,
		"Jun_Weekday_9":  0.03980,
		"Jun_Weekday_10": 0.03841,
		"Jun_Weekday_11": 0.03333,
		"Jun_Weekday_12": 0.03307,
		"Jun_Weekday_13": 0.03329,
		"Jun_Weekday_14": 0.03828,
		"Jun_Weekday_15": 0.03313,
		"Jun_Weekday_16": 0.12609,
		"Jun_Weekday_17": 0.22832,
		"Jun_Weekday_18": 0.27103,
		"Jun_Weekday_19": 0.27007,
		"Jun_Weekday_20": 0.17682,
		"Jun_Weekday_21": 0.08368,
		"Jun_Weekday_22": 0.08121,
		"Jun_Weekday_23": 0.07605,
		"Jun_Weekend_0":  0.07301,
		"Jun_Weekend_1":  0.07103,
		"Jun_Weekend_2":  0.07182,
		"Jun_Weekend_3":  0.07245,
		"Jun_Weekend_4":  0.07070,
		"Jun_Weekend_5":  0.08861,
		"Jun_Weekend_6":  0.07459,
		"Jun_Weekend_7":  0.06233,
		"Jun_Weekend_8":  0.02244,
		"Jun_Weekend_9":  0.01875,
		"Jun_Weekend_10": 0.01930,
		"Jun_Weekend_11": 0.01295,
		"Jun_Weekend_12": 0.01919,
		"Jun_Weekend_13": 0.01523,
		"Jun_Weekend_14": 0.01345,
		"Jun_Weekend_15": 0.01418,
		"Jun_Weekend_16": 0.01508,
		"Jun_Weekend_17": 0.02971,
		"Jun_Weekend_18": 0.07633,
		"Jun_Weekend_19": 0.09656,
		"Jun_Weekend_20": 0.08635,
		"Jun_Weekend_21": 0.08132,
		"Jun_Weekend_22": 0.08055,
		"Jun_Weekend_23": 0.07666,
		"Jul_Weekday_0":  0.07350,
		"Jul_Weekday_1":  0.07518,
		"Jul_Weekday_2":  0.07338,
		"Jul_Weekday_3":  0.07490,
		"Jul_Weekday_4":  0.07174,
		"Jul_Weekday_5":  0.07279,
		"Jul_Weekday_6":  0.07692,
		"Jul_Weekday_7":  0.07364,
		"Jul_Weekday_8":  0.06881,
		"Jul_Weekday_9":  0.06103,
		"Jul_Weekday_10": 0.06353,
		"Jul_Weekday_11": 0.06196,
		"Jul_Weekday_12": 0.05763,
		"Jul_Weekday_13": 0.05429,
		"Jul_Weekday_14": 0.05435,
		"Jul_Weekday_15": 0.05641,
		"Jul_Weekday_16": 0.14115,
		"Jul_Weekday_17": 0.32547,
		"Jul_Weekday_18": 0.36433,
		"Jul_Weekday_19": 0.45746,
		"Jul_Weekday_20": 0.26420,
		"Jul_Weekday_21": 0.08416,
		"Jul_Weekday_22": 0.08230,
		"Jul_Weekday_23": 0.07574,
		"Jul_Weekend_0":  0.08047,
		"Jul_Weekend_1":  0.07483,
		"Jul_Weekend_2":  0.07278,
		"Jul_Weekend_3":  0.07413,
		"Jul_Weekend_4":  0.07221,
		"Jul_Weekend_5":  0.07164,
		"Jul_Weekend_6":  0.06887,
		"Jul_Weekend_7":  0.06700,
		"Jul_Weekend_8":  0.02784,
		"Jul_Weekend_9":  0.02764,
		"Jul_Weekend_10": 0.02919,
		"Jul_Weekend_11": 0.02743,
		"Jul_Weekend_12": 0.02958,
		"Jul_Weekend_13": 0.03225,
		"Jul_Weekend_14": 0.02845,
		"Jul_Weekend_15": 0.03252,
		"Jul_Weekend_16": 0.03290,
		"Jul_Weekend_17": 0.05085,
		"Jul_Weekend_18": 0.09441,
		"Jul_Weekend_19": 0.11911,
		"Jul_Weekend_20": 0.10278,
		"Jul_Weekend_21": 0.08138,
		"Jul_Weekend_22": 0.08032,
		"Jul_Weekend_23": 0.07775,
		"Aug_Weekday_0":  0.09180,
		"Aug_Weekday_1":  0.09199,
		"Aug_Weekday_2":  0.09032,
		"Aug_Weekday_3":  0.08737,
		"Aug_Weekday_4":  0.08694,
		"Aug_Weekday_5":  0.08792,
		"Aug_Weekday_6":  0.08554,
		"Aug_Weekday_7":  0.07465,
		"Aug_Weekday_8":  0.06854,
		"Aug_Weekday_9":  0.06796,
		"Aug_Weekday_10": 0.06765,
		"Aug_Weekday_11": 0.06710,
		"Aug_Weekday_12": 0.06801,
		"Aug_Weekday_13": 0.06842,
		"Aug_Weekday_14": 0.06800,
		"Aug_Weekday_15": 0.07174,
		"Aug_Weekday_16": 0.16432,
		"Aug_Weekday_17": 1.01459,
		"Aug_Weekday_18": 1.13014,
		"Aug_Weekday_19": 1.15441,
		"Aug_Weekday_20": 1.08038,
		"Aug_Weekday_21": 0.89781,
		"Aug_Weekday_22": 0.89024,
		"Aug_Weekday_23": 0.09358,
		"Aug_Weekend_0":  0.08547,
		"Aug_Weekend_1":  0.08447,
		"Aug_Weekend_2":  0.08560,
		"Aug_Weekend_3":  0.08147,
		"Aug_Weekend_4":  0.08079,
		"Aug_Weekend_5":  0.08127,
		"Aug_Weekend_6":  0.07554,
		"Aug_Weekend_7":  0.06223,
		"Aug_Weekend_8":  0.04360,
		"Aug_Weekend_9":  0.03991,
		"Aug_Weekend_10": 0.04048,
		"Aug_Weekend_11": 0.04051,
		"Aug_Weekend_12": 0.03733,
		"Aug_Weekend_13": 0.03699,
		"Aug_Weekend_14": 0.03735,
		"Aug_Weekend_15": 0.04422,
		"Aug_Weekend_16": 0.05947,
		"Aug_Weekend_17": 0.89099,
		"Aug_Weekend_18": 0.94585,
		"Aug_Weekend_19": 1.19289,
		"Aug_Weekend_20": 1.04468,
		"Aug_Weekend_21": 0.95377,
		"Aug_Weekend_22": 0.94703,
		"Aug_Weekend_23": 0.09026,
		"Sep_Weekday_0":  0.09633,
		"Sep_Weekday_1":  0.09442,
		"Sep_Weekday_2":  0.09230,
		"Sep_Weekday_3":  0.08936,
		"Sep_Weekday_4":  0.08798,
		"Sep_Weekday_5":  0.09000,
		"Sep_Weekday_6":  0.08742,
		"Sep_Weekday_7":  0.07786,
		"Sep_Weekday_8":  0.06806,
		"Sep_Weekday_9":  0.06498,
		"Sep_Weekday_10": 0.06332,
		"Sep_Weekday_11": 0.06311,
		"Sep_Weekday_12": 0.06236,
		"Sep_Weekday_13": 0.06155,
		"Sep_Weekday_14": 0.06216,
		"Sep_Weekday_15": 0.06278,
		"Sep_Weekday_16": 0.07673,
		"Sep_Weekday_17": 0.13951,
		"Sep_Weekday_18": 0.46076,
		"Sep_Weekday_19": 0.59505,
		"Sep_Weekday_20": 0.31874,
		"Sep_Weekday_21": 0.15554,
		"Sep_Weekday_22": 0.15520,
		"Sep_Weekday_23": 0.09669,
		"Sep_Weekend_0":  0.08066,
		"Sep_Weekend_1":  0.08440,
		"Sep_Weekend_2":  0.08388,
		"Sep_Weekend_3":  0.08115,
		"Sep_Weekend_4":  0.08115,
		"Sep_Weekend_5":  0.08296,
		"Sep_Weekend_6":  0.08108,
		"Sep_Weekend_7":  0.06615,
		"Sep_Weekend_8":  0.04026,
		"Sep_Weekend_9":  0.02775,
		"Sep_Weekend_10": 0.02527,
		"Sep_Weekend_11": 0.02325,
		"Sep_Weekend_12": 0.02131,
		"Sep_Weekend_13": 0.02232,
		"Sep_Weekend_14": 0.02718,
		"Sep_Weekend_15": 0.02984,
		"Sep_Weekend_16": 0.04376,
		"Sep_Weekend_17": 0.13084,
		"Sep_Weekend_18": 0.53366,
		"Sep_Weekend_19": 0.69865,
		"Sep_Weekend_20": 0.35250,
		"Sep_Weekend_21": 0.16747,
		"Sep_Weekend_22": 0.16773,
		"Sep_Weekend_23": 0.09478,
		"Oct_Weekday_0":  0.09042,
		"Oct_Weekday_1":  0.09043,
		"Oct_Weekday_2":  0.08837,
		"Oct_Weekday_3":  0.08680,
		"Oct_Weekday_4":  0.08602,
		"Oct_Weekday_5":  0.08546,
		"Oct_Weekday_6":  0.08593,
		"Oct_Weekday_7":  0.07750,
		"Oct_Weekday_8":  0.06643,
		"Oct_Weekday_9":  0.06040,
		"Oct_Weekday_10": 0.06077,
		"Oct_Weekday_11": 0.05873,
		"Oct_Weekday_12": 0.05786,
		"Oct_Weekday_13": 0.05731,
		"Oct_Weekday_14": 0.05725,
		"Oct_Weekday_15": 0.05705,
		"Oct_Weekday_16": 0.06290,
		"Oct_Weekday_17": 0.09197,
		"Oct_Weekday_18": 0.09450,
		"Oct_Weekday_19": 0.09297,
		"Oct_Weekday_20": 0.08821,
		"Oct_Weekday_21": 0.08511,
		"Oct_Weekday_22": 0.08979,
		"Oct_Weekday_23": 0.09129,
		"Oct_Weekend_0":  0.07828,
		"Oct_Weekend_1":  0.08054,
		"Oct_Weekend_2":  0.07769,
		"Oct_Weekend_3":  0.07603,
		"Oct_Weekend_4":  0.07992,
		"Oct_Weekend_5":  0.08001,
		"Oct_Weekend_6":  0.07925,
		"Oct_Weekend_7":  0.07097,
		"Oct_Weekend_8":  0.04289,
		"Oct_Weekend_9":  0.02131,
		"Oct_Weekend_10": 0.02111,
		"Oct_Weekend_11": 0.02112,
		"Oct_Weekend_12": 0.02294,
		"Oct_Weekend_13": 0.02058,
		"Oct_Weekend_14": 0.02255,
		"Oct_Weekend_15": 0.02514,
		"Oct_Weekend_16": 0.04282,
		"Oct_Weekend_17": 0.08320,
		"Oct_Weekend_18": 0.08955,
		"Oct_Weekend_19": 0.08594,
		"Oct_Weekend_20": 0.08260,
		"Oct_Weekend_21": 0.07942,
		"Oct_Weekend_22": 0.09283,
		"Oct_Weekend_23": 0.08790,
		"Nov_Weekday_0":  0.08703,
		"Nov_Weekday_1":  0.08807,
		"Nov_Weekday_2":  0.08653,
		"Nov_Weekday_3":  0.08527,
		"Nov_Weekday_4":  0.08648,
		"Nov_Weekday_5":  0.08688,
		"Nov_Weekday_6":  0.08338,
		"Nov_Weekday_7":  0.07373,
		"Nov_Weekday_8":  0.06343,
		"Nov_Weekday_9":  0.06084,
		"Nov_Weekday_10": 0.05983,
		"Nov_Weekday_11": 0.05806,
		"Nov_Weekday_12": 0.05631,
		"Nov_Weekday_13": 0.05057,
		"Nov_Weekday_14": 0.05169,
		"Nov_Weekday_15": 0.06617,
		"Nov_Weekday_16": 0.09378,
		"Nov_Weekday_17": 0.09811,
		"Nov_Weekday_18": 0.08995,
		"Nov_Weekday_19": 0.08709,
		"Nov_Weekday_20": 0.08620,
		"Nov_Weekday_21": 0.08693,
		"Nov_Weekday_22": 0.08795,
		"Nov_Weekday_23": 0.09317,
		"Nov_Weekend_0":  0.08425,
		"Nov_Weekend_1":  0.08110,
		"Nov_Weekend_2":  0.07919,
		"Nov_Weekend_3":  0.08099,
		"Nov_Weekend_4":  0.08262,
		"Nov_Weekend_5":  0.08530,
		"Nov_Weekend_6":  0.08354,
		"Nov_Weekend_7":  0.06369,
		"Nov_Weekend_8":  0.04400,
		"Nov_Weekend_9":  0.03552,
		"Nov_Weekend_10": 0.03225,
		"Nov_Weekend_11": 0.02967,
		"Nov_Weekend_12": 0.02805,
		"Nov_Weekend_13": 0.02590,
		"Nov_Weekend_14": 0.02223,
		"Nov_Weekend_15": 0.04528,
		"Nov_Weekend_16": 0.08231,
		"Nov_Weekend_17": 0.09327,
		"Nov_Weekend_18": 0.08755,
		"Nov_Weekend_19": 0.08551,
		"Nov_Weekend_20": 0.08449,
		"Nov_Weekend_21": 0.08344,
		"Nov_Weekend_22": 0.08761,
		"Nov_Weekend_23": 0.08532,
		"Dec_Weekday_0":  0.09049,
		"Dec_Weekday_1":  0.09002,
		"Dec_Weekday_2":  0.08769,
		"Dec_Weekday_3":  0.08836,
		"Dec_Weekday_4":  0.08966,
		"Dec_Weekday_5":  0.09274,
		"Dec_Weekday_6":  0.09964,
		"Dec_Weekday_7":  0.08758,
		"Dec_Weekday_8":  0.07551,
		"Dec_Weekday_9":  0.07085,
		"Dec_Weekday_10": 0.07003,
		"Dec_Weekday_11": 0.06603,
		"Dec_Weekday_12": 0.06341,
		"Dec_Weekday_13": 0.06270,
		"Dec_Weekday_14": 0.06390,
		"Dec_Weekday_15": 0.07224,
		"Dec_Weekday_16": 0.09291,
		"Dec_Weekday_17": 0.09820,
		"Dec_Weekday_18": 0.09818,
		"Dec_Weekday_19": 0.09082,
		"Dec_Weekday_20": 0.09057,
		"Dec_Weekday_21": 0.09245,
		"Dec_Weekday_22": 0.09265,
		"Dec_Weekday_23": 0.09135,
		"Dec_Weekend_0":  0.08404,
		"Dec_Weekend_1":  0.08127,
		"Dec_Weekend_2":  0.07761,
		"Dec_Weekend_3":  0.07374,
		"Dec_Weekend_4":  0.07407,
		"Dec_Weekend_5":  0.07599,
		"Dec_Weekend_6":  0.07851,
		"Dec_Weekend_7":  0.06743,
		"Dec_Weekend_8":  0.04497,
		"Dec_Weekend_9":  0.02337,
		"Dec_Weekend_10": 0.02031,
		"Dec_Weekend_11": 0.02105,
		"Dec_Weekend_12": 0.02259,
		"Dec_Weekend_13": 0.01642,
		"Dec_Weekend_14": 0.01568,
		"Dec_Weekend_15": 0.04292,
		"Dec_Weekend_16": 0.08722,
		"Dec_Weekend_17": 0.08905,
		"Dec_Weekend_18": 0.08622,
		"Dec_Weekend_19": 0.08095,
		"Dec_Weekend_20": 0.08173,
		"Dec_Weekend_21": 0.08203,
		"Dec_Weekend_22": 0.08361,
		"Dec_Weekend_23": 0.08287,
	},
}

// getPGEHolidays calculates designated holidays for PG&E.
// The holidays observed are:
// 1. New Year's Day (January 1)
// 2. Presidents' Day (Third Monday in February)
// 3. Memorial Day (Last Monday in May)
// 4. Independence Day (July 4)
// 5. Labor Day (First Monday in September)
// 6. Veterans Day (November 11)
// 7. Thanksgiving Day (Fourth Thursday in November)
// 8. Christmas Day (December 25)
//
// Shift rules:
// - If a holiday falls on a Sunday, the following Monday is recognized as the holiday.
// - If a holiday falls on a Saturday, the Friday before is recognized as the holiday.
func getPGEHolidays(year int) []string {
	shiftPGEWeekendHoliday := func(t time.Time) time.Time {
		switch t.Weekday() {
		case time.Saturday:
			return t.AddDate(0, 0, -1)
		case time.Sunday:
			return t.AddDate(0, 0, 1)
		default:
			return t
		}
	}

	holidays := []time.Time{
		shiftPGEWeekendHoliday(newYearsDay(year)),
		shiftPGEWeekendHoliday(presidentsDay(year)),
		memorialDay(year),
		shiftPGEWeekendHoliday(independenceDay(year)),
		laborDay(year),
		shiftPGEWeekendHoliday(veteransDay(year)),
		thanksgivingDay(year),
		shiftPGEWeekendHoliday(christmasDay(year)),
		shiftPGEWeekendHoliday(newYearsDay(year + 1)),
	}

	return formatHolidays(holidays, year)
}

// pgEPeriods generates the Pricing periods for PG&E (Pacific Gas & Electric).
func pgEPeriods(plan string, options types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	for _, year := range years {
		holidays := getPGEHolidays(year)

		switch plan {
		case "pg_e_e1":
			// E-1 (Standard Residential)
			// Total Bundled Tier 1: $0.32561
			// Base = $0.32561 - $0.01230 (NBC) = $0.31331
			// Note: We assume the customer stays within the baseline and therefore we apply the baseline credit rate.
			periods = append(periods, buildPeriods(ptLocation, []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.January,
					MonthEnd:           time.December,
					OtherDollarsPerKWH: 0.32561 - pgeNBC,
					OtherDescription:   "PG&E E-1 Base Rate",
				},
			})...)

		case "pg_e_e_tou_c":
			// E-TOU-C (Peak 4-9 PM Everyday)
			// Summer (June - Sept): Peak = $0.52240, Off-Peak = $0.39940. Baseline credit = ($0.08140)
			// Winter (Oct - May): Peak = $0.39757, Off-Peak = $0.36757. Baseline credit = ($0.08140)
			// Note: We assume the customer stays within the baseline and therefore we apply the baseline credit subtraction.
			touCBaselineCredit := 0.08140
			periods = append(periods, buildPeriods(ptLocation, []touSimplifiedPeriod{
				// Summer (June - Sept)
				{
					Year:       year,
					MonthStart: time.June,
					MonthEnd:   time.September,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 21}},
							DollarsPerKWH: 0.52240 - touCBaselineCredit - pgeNBC,
							Description:   "E-TOU-C Summer Peak (4-9 PM)",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.39940 - touCBaselineCredit - pgeNBC,
					OtherDescription:   "E-TOU-C Summer Off-Peak",
				},
				// Winter (Oct - May)
				{
					Year:       year,
					MonthStart: time.October,
					MonthEnd:   time.May,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 21}},
							DollarsPerKWH: 0.39757 - touCBaselineCredit - pgeNBC,
							Description:   "E-TOU-C Winter Peak (4-9 PM)",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.36757 - touCBaselineCredit - pgeNBC,
					OtherDescription:   "E-TOU-C Winter Off-Peak",
				},
			})...)

		case "pg_e_e_tou_d":
			// E-TOU-D (Peak 5-8 PM Weekdays, Off-Peak weekends/holidays)
			// Summer (June - Sept): Peak = $0.47708, Off-Peak = $0.34212
			// Winter (Oct - May): Peak = $0.38747, Off-Peak = $0.34886
			periods = append(periods, buildPeriods(ptLocation, []touSimplifiedPeriod{
				// Summer (June - Sept) - Holidays (Off-Peak all day)
				{
					Year:               year,
					MonthStart:         time.June,
					MonthEnd:           time.September,
					SpecificDates:      holidays,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.34212 - pgeNBC,
					OtherDescription:   "E-TOU-D Summer Holiday Off-Peak",
				},
				// Summer (June - Sept) - Regular Days
				{
					Year:             year,
					MonthStart:       time.June,
					MonthEnd:         time.September,
					SpecificDates:    holidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 17, HourEnd: 20}},
							Weekday:       true,
							DollarsPerKWH: 0.47708 - pgeNBC,
							Description:   "E-TOU-D Summer Weekday Peak (5-8 PM)",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.34212 - pgeNBC,
					OtherDescription:   "E-TOU-D Summer Off-Peak",
				},
				// Winter (Oct - May) - Holidays (Off-Peak all day)
				{
					Year:               year,
					MonthStart:         time.October,
					MonthEnd:           time.May,
					SpecificDates:      holidays,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.34886 - pgeNBC,
					OtherDescription:   "E-TOU-D Winter Holiday Off-Peak",
				},
				// Winter (Oct - May) - Regular Days
				{
					Year:             year,
					MonthStart:       time.October,
					MonthEnd:         time.May,
					SpecificDates:    holidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 17, HourEnd: 20}},
							Weekday:       true,
							DollarsPerKWH: 0.38747 - pgeNBC,
							Description:   "E-TOU-D Winter Weekday Peak (5-8 PM)",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.34886 - pgeNBC,
					OtherDescription:   "E-TOU-D Winter Off-Peak",
				},
			})...)

		case "pg_e_e_elec":
			// E-ELEC (Electric Home TOU)
			// Summer (June - Sept): Peak (4-9 PM) = $0.55214, Part-Peak (3-4 PM & 9-12 AM) = $0.39026, Off-Peak = $0.33358
			// Winter (Oct - May): Peak (4-9 PM) = $0.32063, Part-Peak (3-4 PM & 9-12 AM) = $0.29854, Off-Peak = $0.28468
			periods = append(periods, buildPeriods(ptLocation, []touSimplifiedPeriod{
				// Summer (June - Sept)
				{
					Year:       year,
					MonthStart: time.June,
					MonthEnd:   time.September,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 21}},
							DollarsPerKWH: 0.55214 - pgeNBC,
							Description:   "E-ELEC Summer Peak (4-9 PM)",
						},
						{
							Name: "Partial Peak",
							Hours: []types.UtilityHourPeriod{
								{HourStart: 15, HourEnd: 16},
								{HourStart: 21, HourEnd: 24},
							},
							DollarsPerKWH: 0.39026 - pgeNBC,
							Description:   "E-ELEC Summer Part-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.33358 - pgeNBC,
					OtherDescription:   "E-ELEC Summer Off-Peak",
				},
				// Winter (Oct - May)
				{
					Year:       year,
					MonthStart: time.October,
					MonthEnd:   time.May,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 21}},
							DollarsPerKWH: 0.32063 - pgeNBC,
							Description:   "E-ELEC Winter Peak (4-9 PM)",
						},
						{
							Name: "Partial Peak",
							Hours: []types.UtilityHourPeriod{
								{HourStart: 15, HourEnd: 16},
								{HourStart: 21, HourEnd: 24},
							},
							DollarsPerKWH: 0.29854 - pgeNBC,
							Description:   "E-ELEC Winter Part-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.28468 - pgeNBC,
					OtherDescription:   "E-ELEC Winter Off-Peak",
				},
			})...)

		case "pg_e_ev2":
			// EV2-A (EV Home Charging)
			// Summer (June - Sept): Peak (4-9 PM) = $0.53809, Part-Peak (3-4 PM & 9-12 AM) = $0.42760, Off-Peak = $0.22558
			// Winter (Oct - May): Peak (4-9 PM) = $0.41099, Part-Peak (3-4 PM & 9-12 AM) = $0.39428, Off-Peak = $0.22558
			periods = append(periods, buildPeriods(ptLocation, []touSimplifiedPeriod{
				// Summer (June - Sept)
				{
					Year:       year,
					MonthStart: time.June,
					MonthEnd:   time.September,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 21}},
							DollarsPerKWH: 0.53809 - pgeNBC,
							Description:   "EV2 Summer Peak (4-9 PM)",
						},
						{
							Name: "Partial Peak",
							Hours: []types.UtilityHourPeriod{
								{HourStart: 15, HourEnd: 16},
								{HourStart: 21, HourEnd: 24},
							},
							DollarsPerKWH: 0.42760 - pgeNBC,
							Description:   "EV2 Summer Part-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.22558 - pgeNBC,
					OtherDescription:   "EV2 Summer Off-Peak",
				},
				// Winter (Oct - May)
				{
					Year:       year,
					MonthStart: time.October,
					MonthEnd:   time.May,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 21}},
							DollarsPerKWH: 0.41099 - pgeNBC,
							Description:   "EV2 Winter Peak (4-9 PM)",
						},
						{
							Name: "Partial Peak",
							Hours: []types.UtilityHourPeriod{
								{HourStart: 15, HourEnd: 16},
								{HourStart: 21, HourEnd: 24},
							},
							DollarsPerKWH: 0.39428 - pgeNBC,
							Description:   "EV2 Winter Part-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.22558 - pgeNBC,
					OtherDescription:   "EV2 Winter Off-Peak",
				},
			})...)
		}
	}

	// Add Non-Bypassable Charges (NBCs) unconditionally for all PG&E rates as GridUse fee.
	periods = append(periods, types.UtilityFeesPeriod{
		TimePeriod: types.TimePeriod{
			LocationPtr: ptLocation,
		},
		DollarsPerKWH:  pgeNBC,
		GridAdditional: true,
		Description:    "PG&E Non-Bypassable Charges",
	})

	// Add dynamic NBT export rates for SBP scheme (NEM 3.0)
	if options.NetMeteringScheme == "sbp" || options.NetMeteringScheme == "" {
		locPtr := ptLocation
		for _, year := range years {
			holidays := getPGEHolidays(year)
			for month := time.January; month <= time.December; month++ {
				startMonth := time.Date(year, month, 1, 0, 0, 0, 0, locPtr)
				endMonth := time.Date(year, month+1, 1, 0, 0, 0, 0, locPtr)
				monthStr := month.String()[:3]

				for hour := range 24 {
					// 1. Weekday non-holiday period
					weekdayKey := fmt.Sprintf("%s_Weekday_%d", monthStr, hour)
					var weekdayVal float64
					if yearMap, ok := pgeNBTData[year]; ok {
						weekdayVal = yearMap[weekdayKey]
					}
					if weekdayVal == 0 {
						weekdayVal = pgeNBTData[2026][weekdayKey]
					}

					periods = append(periods, types.UtilityFeesPeriod{
						TimePeriod: types.TimePeriod{
							Start:            startMonth,
							End:              endMonth,
							LocationPtr:      locPtr,
							DaysOfTheWeek:    []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
							SpecificDates:    holidays,
							SpecificDatesNot: true,
							Hours:            []types.UtilityHourPeriod{{HourStart: hour, HourEnd: hour + 1}},
						},
						DollarsPerKWH:            weekdayVal,
						SeparateGenerationCredit: true,
						Description:              fmt.Sprintf("PG&E NBT Weekday Export Credit (Hour %d)", hour),
					})

					// 2. Weekend period
					weekendKey := fmt.Sprintf("%s_Weekend_%d", monthStr, hour)
					var weekendVal float64
					if yearMap, ok := pgeNBTData[year]; ok {
						weekendVal = yearMap[weekendKey]
					}
					if weekendVal == 0 {
						weekendVal = pgeNBTData[2026][weekendKey]
					}

					periods = append(periods, types.UtilityFeesPeriod{
						TimePeriod: types.TimePeriod{
							Start:         startMonth,
							End:           endMonth,
							LocationPtr:   locPtr,
							DaysOfTheWeek: []time.Weekday{time.Saturday, time.Sunday},
							Hours:         []types.UtilityHourPeriod{{HourStart: hour, HourEnd: hour + 1}},
						},
						DollarsPerKWH:            weekendVal,
						SeparateGenerationCredit: true,
						Description:              fmt.Sprintf("PG&E NBT Weekend Export Credit (Hour %d)", hour),
					})

					// 3. Holiday period (restrict to weekdays Monday-Friday)
					periods = append(periods, types.UtilityFeesPeriod{
						TimePeriod: types.TimePeriod{
							Start:            startMonth,
							End:              endMonth,
							LocationPtr:      locPtr,
							DaysOfTheWeek:    []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
							SpecificDates:    holidays,
							SpecificDatesNot: false,
							Hours:            []types.UtilityHourPeriod{{HourStart: hour, HourEnd: hour + 1}},
						},
						DollarsPerKWH:            weekendVal, // Holidays use weekend rates
						SeparateGenerationCredit: true,
						Description:              fmt.Sprintf("PG&E NBT Holiday Export Credit (Hour %d)", hour),
					})
				}
			}
		}
	}

	return periods
}

// pgEUtilityInfo returns the utility info for PG&E.
func pgEUtilityInfo() types.UtilityProviderInfo {
	return types.UtilityProviderInfo{
		ID:   "pg_e",
		Name: "Pacific Gas and Electric (PG&E)",
		Rates: []types.UtilityRateInfo{
			{
				ID:   "pg_e_e1",
				Name: "Standard Residential (E-1)",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringScheme",
						Name:        "Net Metering / Export Scheme",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your net metering or solar billing plan program.",
						Choices: []types.UtilityOptionChoice{
							{Value: "net", Name: "NEM 1.0 (installs before June 2016)"},
							{Value: "nem2", Name: "NEM 2.0 (installs between June 2016 and April 15, 2023)"},
							{Value: "sbp", Name: "Solar Billing Plan (NEM 3.0) (installs after April 15, 2023)"},
						},
						Default: "sbp",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return pgEPeriods("pg_e_e1", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "pg_e_e_tou_c",
				Name: "Time-of-Use Peak 4-9 PM Everyday (E-TOU-C)",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringScheme",
						Name:        "Net Metering / Export Scheme",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your net metering or solar billing plan program.",
						Choices: []types.UtilityOptionChoice{
							{Value: "net", Name: "NEM 1.0 (installs before June 2016)"},
							{Value: "nem2", Name: "NEM 2.0 (installs between June 2016 and April 15, 2023)"},
							{Value: "sbp", Name: "Solar Billing Plan (NEM 3.0) (installs after April 15, 2023)"},
						},
						Default: "sbp",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return pgEPeriods("pg_e_e_tou_c", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "pg_e_e_tou_d",
				Name: "Time-of-Use Peak 5-8 PM Weekdays (E-TOU-D)",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringScheme",
						Name:        "Net Metering / Export Scheme",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your net metering or solar billing plan program.",
						Choices: []types.UtilityOptionChoice{
							{Value: "net", Name: "NEM 1.0 (installs before June 2016)"},
							{Value: "nem2", Name: "NEM 2.0 (installs between June 2016 and April 15, 2023)"},
							{Value: "sbp", Name: "Solar Billing Plan (NEM 3.0) (installs after April 15, 2023)"},
						},
						Default: "sbp",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return pgEPeriods("pg_e_e_tou_d", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "pg_e_e_elec",
				Name: "Electric Home (E-ELEC)",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringScheme",
						Name:        "Net Metering / Export Scheme",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your net metering or solar billing plan program.",
						Choices: []types.UtilityOptionChoice{
							{Value: "net", Name: "NEM 1.0 (installs before June 2016)"},
							{Value: "nem2", Name: "NEM 2.0 (installs between June 2016 and April 15, 2023)"},
							{Value: "sbp", Name: "Solar Billing Plan (NEM 3.0) (installs after April 15, 2023)"},
						},
						Default: "sbp",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return pgEPeriods("pg_e_e_elec", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "pg_e_ev2",
				Name: "EV Home Charging (EV2-A)",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringScheme",
						Name:        "Net Metering / Export Scheme",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your net metering or solar billing plan program.",
						Choices: []types.UtilityOptionChoice{
							{Value: "net", Name: "NEM 1.0 (installs before June 2016)"},
							{Value: "nem2", Name: "NEM 2.0 (installs between June 2016 and April 15, 2023)"},
							{Value: "sbp", Name: "Solar Billing Plan (NEM 3.0) (installs after April 15, 2023)"},
						},
						Default: "sbp",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return pgEPeriods("pg_e_ev2", opts, []int{2026, 2027}), nil
				},
			},
		},
	}
}
