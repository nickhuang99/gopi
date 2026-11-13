package main

// chat_status_cache_bound.go — the BOUNDED count form the cache pair is drawn in
// (epic 01a0e2e9).
//
// The defect this file exists to make unreachable: a status field whose rendered
// width is a function of the MAGNITUDE of its input. statusCacheField composed the
// pair from an unbounded formatter ("↑1662k↓1662k", twelve cells at the operator's
// 1.66M-token session) inside a sub-cell whose reservation had been measured from
// one small string ("↑12k↓4k", seven cells), so the layout's own field cut took the
// five cells that were missing — off the TAIL, which is where the ↓ arrow and the
// cache-WRITE number live. The side billed at the MISS rate was the side swallowed,
// leaving a paired read-out that read as if it were the whole story.
//
// Two things had to change at once, and neither is sufficient alone:
//
//	1. THE FORM. formatCountBounded's output cannot exceed a stated cell width, for
//	   ANY int — so a reservation can be a TRUE statement about it (boundedCount
//	   below; the bound is proven by TestStatusCacheFieldWidthIsBounded, which walks
//	   every rung boundary and the extremes of the type).
//	2. THE RESERVATION. statusCacheCol is derived from that width, so the sub-cell
//	   holds the worst case by arithmetic rather than by sampling (see its note).
//
// The bounded form trades PRECISION for WIDTH, which is the trade the row already
// makes elsewhere (formatCount prints "42.1k" where 42100 would cost five cells):
// one tenth of the unit shown, an approximation whose unit is stated. It never
// TRUNCATES a number: below the first scale the value is printed whole, and where a
// cap does bind the rendering is a ROUNDED figure (with the unit that makes the
// rounding visible) — never a clipped one.

// boundedCountUnit is one rung of the bounded formatter's scale ladder: the unit
// the count is printed in, and the power of ten that unit stands for. The ladder is
// what makes the width finite — a value's magnitude selects a RUNGS INDEX, and the
// index cannot exceed the ladder's length, so no count can spell more units than
// these four.
type boundedCountUnit struct {
	unit  string
	scale int
}

// statusCountUnits is the ladder, from whole numbers to the widest unit. It is the
// formatter's single source of magnitude (Rule 14): the rungs, the reservation
// derived from them (statusCacheCountUnitWidest) and the compact-fit predicate all
// read THIS list, so a unit added here is a unit the geometry learns about in the
// same change.
//
// The rungs are the decimal ladder the row's other counts already climb
// (statusCountCompactKilo is its first step). statusCountMaxDigits is what bounds a
// side's width on any of them, so the ladder does not have to be long: the digits
// cap per rung, and a value larger than a rung can express is printed at that rung
// ROUNDED (see boundedCount's cap branch), which is why no value can lengthen its
// own rendering.
var statusCountUnits = []boundedCountUnit{
	{unit: "", scale: 1},
	{unit: "k", scale: statusCountCompactKilo},
	{unit: "M", scale: statusCountCompactKilo * statusCountCompactKilo},
	{unit: "G", scale: statusCountCompactKilo * statusCountCompactKilo * statusCountCompactKilo},
}

// countScaleRung is the rung a count belongs on: the LARGEST rung whose scale the
// value reaches. A value below the first scale answers rung 0 (the whole numbers),
// which is also the rung an unmeasured or negative count lands on.
//
// It is a function rather than an inline loop because the RUNG DECIDES THE WIDTH (a
// side's cost is the rung's unit plus the digits printed against it), so a second
// caller — the geometry's widest-case helper — has to resolve it exactly as the
// formatter does; two copies of this scan would be two descriptions of the row.
func countScaleRung(n int) (rung int, scale int) {
	for i := len(statusCountUnits) - 1; i > 0; i-- {
		if n >= statusCountUnits[i].scale {
			return i, statusCountUnits[i].scale
		}
	}
	return 0, statusCountUnits[0].scale
}

// leadingDigits renders at most limit of a whole number's decimal digits, most
// significant first — the ONE function on this row that turns a number into its
// decimal spelling, so "how many digits does this print" is a measurement of a
// string rather than a second arithmetic that could disagree with it.
//
// A limit of 0 (or less) means NO limit. The limit exists for the cap: a whole part
// wider than it is printed at the limit, because the caller has already ROUNDED the
// value there and the digits left off are the ones the rounding decided. A shorter
// number is printed WHOLE, never zero-padded — padding spends cells saying nothing,
// and a padded "0012k" claims a precision the counter does not have.
//
// A non-positive number prints "0": the formatter's domain is a token count, and a
// value outside it is reported as the zero it cannot be told apart from (the pair is
// empty for an unmeasured side — statusCacheField).
func leadingDigits(v, limit int) string {
	if v <= 0 {
		return statusCountZero
	}
	var buf [statusCountDigitsBuf]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	digits := buf[i:]
	// Keep the MOST SIGNIFICANT digits: the leading ones state the magnitude, and a
	// tail cut here would keep a value's noise and drop its scale.
	if limit > 0 && len(digits) > limit {
		digits = digits[:limit]
	}
	return string(digits)
}

// statusCountSmallestMantissa is the smallest whole part a UNIT may be printed with:
// the figure the bounded form states when a value's whole part rounds away, so that no
// magnitude is ever drawn as a bare unit glyph ("0M", "0G"). ONE, and it is derived
// rather than chosen — it is the first whole number, which is the value a rung's scale
// divides a count up to the moment the count reaches that scale, and it is the same
// figure the form already prints one boundary away (999999999 is "1G").
//
// It costs a cell only in the sense every rung figure does: one digit plus the unit is
// inside statusCountSideTextWidest, so the reservation is unaffected (Rule 13).
const statusCountSmallestMantissa = 1

// statusCountZero is a count of zero's spelling, named because it is produced on
// more than one path (the formatter's own zero, and the digit count's floor) and a
// bare "0" in either would be a second definition of the same rendering (Rule 1).
const statusCountZero = "0"

// countDecimalDigits is the number of decimal digits in a whole number, with 0
// counted as one digit.
//
// It is DERIVED from the ONE function that turns a number into its decimal spelling
// (leadingDigits, with no limit) rather than counted with a second loop of its own,
// because the two numbers are the same fact used in two places: the formatter asks
// "how many digits is this value", and the geometry asks "how many digits can a side
// print". Two implementations of one count is precisely the shape of this epic's
// defect — a reservation measured against one spelling of a value while the
// formatter produced another (Rule 14).
func countDecimalDigits(n int) int {
	return len(leadingDigits(n, 0))
}

// boundedCount is a count in the bounded form, plus the two facts a caller may
// assert about it: its cell width (the quantity the reservation is stated against)
// and the unit it was printed in.
//
// The width is CARRIED rather than recomputed by the caller because it is what the
// geometry depends on, and a test that re-derived it with its own arithmetic would
// prove nothing about the string the row draws (Rule 31: the surface is executed).
type boundedCount struct {
	text string
	// cells is text's width in DISPLAY CELLS, measured by the same function the
	// row's geometry uses for every other field (statusFieldCells).
	cells int
	// unit is the rung the count printed in — the fact that makes the rounding
	// HONEST rather than a truncation (the reader can see the order of magnitude the
	// number was rounded to).
	unit string
}

// boundedCountRect formats n in the bounded form. Its output width is bounded by
// statusCacheCountSideCellsWidest for EVERY int, and that is the property the
// sub-cell's reservation is derived from.
//
// The form, in order:
//
//	below the first scale   the value is printed WHOLE ("999"). No unit is invented
//	                        and no rounding is applied, because neither is needed:
//	                        the first scale is 1000, so an integer below it cannot
//	                        reach the digits cap at all.
//	on a rung               the value is printed in that rung's unit with
//	                        statusCountFractionDigits decimals ("166.2k", "12.3k"),
//	                        and WHOLE when the tenth rounds to zero ("12k") — the
//	                        same shape the row's other counts have, and the reason a
//	                        cell is not spent on a trailing ".0".
//	at the digits cap       a value that the widest unit on the ladder still cannot
//	                        state as a whole number (a trillion-fold count) is
//	                        printed ROUNDED at the cap, in that unit ("1000G" for
//	                        1e12 and up). Rounding, not clipping: the digits it
//	                        stands in for are stated by the unit, and the result is
//	                        an approximation of what the counter holds — the
//	                        alternative, a clipped leading prefix, prints a number
//	                        the counter never reported, which is the one thing this
//	                        row's honesty rule forbids.
//
// THE ORDER OF THE THREE DECISIONS IS THE WHOLE OF ITS CORRECTNESS, and it is stated
// here because two earlier drafts got it wrong in ways worth naming. The value is
// (1) resolved to its rung, (2) rounded ONCE — at the precision the rung's unit can
// carry, one tenth — with the carry applied to the whole part, and (3) capped only
// if the whole part is STILL wider than a side may print, which now happens only for
// counts past the ladder's widest unit.
//
// The drafts that failed, and why this order is the only one that works: capping the
// whole part BEFORE rounding printed 999999999 as "10M" (two roundings of one value
// at two precisions do not compose — the carry has to be applied BEFORE the cap, not
// instead of it); letting the carry produce "1000.0k" printed a magnitude in a unit
// the value had already outgrown (the rung must be re-resolved once the carry is
// known); and treating rung zero as a rung printed "999" as "99" (the whole-number
// class is not a unit, so neither rounding nor the cap applies to it).
func boundedCountRect(n int) boundedCount {
	return boundedCountRectDigits(n, statusCountWholeDigitsWidest)
}

// boundedCountRectDigits is boundedCountRect at a CALLER-CHOSEN digit allowance.
// The ladder, the rounding rule, the carry and the zero-mantissa repair are the
// SAME ones (this is the only body; boundedCountRect is a one-line call into it), so
// a second caller cannot drift from the audited form — only the number of digits a
// rung may print changes, which is exactly the quantity a narrower or wider FIELD
// buys. It exists for the context pair's fine form (statusCtxPairFine), whose side
// budget is not the cache pair's: the cache sides carry an arrow, the context sides
// carry none, so the same cell count buys one more digit there.
//
// maxDigits is a CAP on the whole part, never a license to clip: every value is
// still ROUNDED (the unit states the precision), and a value the allowance cannot
// state is printed at the allowance rounded — see the cap branch below, unchanged
// from the single-argument form.
func boundedCountRectDigits(n int, maxDigits int) boundedCount {
	if n < 0 {
		// A negative counter is not a measurement this transport reports (token
		// volumes are counts). The row must not print one as if it were: the pair is
		// empty for an unmeasured side (statusCacheField), so a negative is reported
		// as the zero it cannot be told apart from.
		n = 0
	}
	rung, scale := countScaleRung(n)
	// RUNG ZERO IS THE WHOLE NUMBERS, and it is not a unit: below the first scale the
	// value is printed exactly as it is. There is no rounding to apply (an integer
	// needs no tenth) and no cap to bind — the scale is the cap's own base, so a value
	// under it cannot have more digits than the cap allows. Handling it HERE rather
	// than as a rung with an empty unit is what keeps "999" printing as "999".
	if rung == 0 {
		// No limit is passed (leadingDigits' 0 = "all digits"): the cap is stated
		// against the UNITS, and an integer below the first scale is not capped at all
		// — statusCountDigitsPerRung is the scale's base, so a value under it can never
		// exceed the cap, and passing the cap here would cut "999" to "99".
		return newBoundedCount(leadingDigits(n, 0), "")
	}
	// The value is printed scaled to its rung's unit, ROUNDED to
	// statusCountFractionDigits decimals of that unit, with the carry of that rounding
	// applied to the whole part — and, when the carry reaches the next scale, printed
	// in the unit the value has actually reached. Two facts decide it, and both are
	// properties of rounding rather than special cases:
	//
	//  1. ROUNDING IS APPLIED EXACTLY ONCE PER UNIT, and its carry goes into the WHOLE
	//     part before anything is printed — never printed as a ten-tenths decimal
	//     ("0.10M"). Computing the fraction by TRUNCATING the remainder instead rounds
	//     the count down silently ("1662000" would print "1.6M" where the honest
	//     one-decimal form is "1.7M"): the approximation has to be stated by the unit,
	//     never hidden in a digit.
	//  2. THE CARRY CAN MOVE THE VALUE TO THE NEXT RUNG, and the rung is what states a
	//     magnitude. 99999 at the kilo rung rounds to 100k and fits there; 999999 rounds
	//     to 1000.0k, which is exactly 1M — a value the ladder has a name for, and one
	//     the kilo rung would report in a unit it has left. So the value is re-scaled in
	//     the next unit whenever the rounding carries into it.
	//
	// The loop is bounded by len(statusCountUnits) — every pass advances the rung index
	// and starts from the value, so the LADDER's length is what terminates it and no
	// count can lengthen its own rendering. A bound a value could satisfy would be a
	// width a value could grow, which is the defect this form exists to close.
	var whole, frac int
	var unit string
	for rung < len(statusCountUnits)-1 {
		scale = statusCountUnits[rung].scale
		whole, frac = countRungSplit(n, scale)
		if frac == statusCountFractionScale {
			whole++
		}
		if countDecimalDigits(whole) <= maxDigits {
			// The rounded value still belongs to this rung: its whole part fits the digits
			// the rung may print. The test is on the DIGITS rather than on the next scale
			// because the two are different boundaries — 99.9k rounds to 100k, which is
			// under the next scale but needs THREE digits, so it has to be printed as the
			// next unit's value (0.1M, and with no fraction, 0.1M rounds to 0) rather than
			// as a wider number on this one.
			break
		}
		rung++
	}
	// The top rung has no rung above it to re-resolve against, so the loop above stops
	// there without having split the value in it: split once, here, in whatever unit the
	// loop settled on. (Doing it unconditionally rather than in the loop's exit path
	// keeps the split in ONE place -- a second copy would be a second rounding rule.)
	scale = statusCountUnits[rung].scale
	unit = statusCountUnits[rung].unit
	whole, frac = countRungSplit(n, scale)
	if frac == statusCountFractionScale {
		whole++
		frac = 0
	}
	if countDecimalDigits(whole)+statusCountFractionSepCells+statusCountFractionDigits > statusCountSideTextWidest {
		// The whole part is already wide enough that the fraction would put the side past
		// the text the reservation states: the value is printed as the whole number it
		// rounds to, in the unit that makes the rounding visible. Dropping the fraction
		// costs nothing a reader needs — the unit states the same magnitude — and keeping
		// it would be a side the geometry did not reserve.
		//
		// The check is on the SUM (whole part + separator + fraction) rather than on either
		// number alone, because that sum is the width the reservation is stated against.
		frac = 0
	}
	if whole == 0 {
		// THE BARE-UNIT GLYPH IS MADE UNREACHABLE HERE ("0M", "0G").
		//
		// A whole part of zero with a unit attached names an order of magnitude and NO
		// quantity, and the row draws it inside a pair that presents it as a measurement
		// (statusCacheField's "↑…↓…"), so the reader takes a reading that is not a reading.
		//
		// THE BAND IS WIDE, NOT A BOUNDARY CASE, which is why this is a repair rather than a
		// guard against a rounding artefact. statusCountFractionDigits is ZERO (the precision
		// the row's budget buys: a decimal costs the separator plus its digit on a side that
		// appears TWICE), so a rung whose whole part rounds to zero prints the unit ALONE. The
		// climb above reaches that rung for every value whose next-lower rung overflowed
		// statusCountWholeDigitsWidest — so the pre-fix form rendered the WHOLE BAND
		// [100000..499999] as "0M" and [100000000..499999999] as "0G": a hundred thousand
		// tokens drawn as a magnitude with no number.
		//
		// THE REMEDY FITS THE GEOMETRY MASTER ALREADY OWNS, WHICH IS THE POINT. The branch this
		// epic inherits from wanted FOUR text cells (a decimal: "1.7M") and paid for them by
		// re-partitioning the mode column 5->3 — a claim MEASURED FALSE then (its field was
		// "tools", five cells, against a five-cell column: zero slack) and void NOW in a
		// second way (task 01a0e4c7 RETIRED that column, absorbing its cells into
		// statusToolCol — see statusModeColRetired — so there is no mode reservation left to
		// re-partition). So the fix is NOT a wider spelling: it is the smallest mantissa the
		// rung can state, which is ONE — the same width as any other rung figure
		// (statusCountWholeDigitsWidest digits plus the unit, i.e. inside
		// statusCountSideTextWidest), so statusCacheCol stays 8, statusPinCols stays 100 and
		// statusPinSlackCells stays 0.
		//
		// It is also the behaviour the form ALREADY has one boundary away: 999999999 is printed
		// "1G" by the carry path above, and 500000 "1M". Clamping the underflow to the same
		// figure makes the scale TOTAL — a rung states either the whole part it rounds to or,
		// when that rounds away, the smallest figure the unit has — instead of stating a unit
		// with nothing in it. The reader keeps the order of magnitude, which is the fact the
		// bounded form trades precision for, and stops being shown a number that is not one.
		whole = statusCountSmallestMantissa
	}
	// THE WIDTH BOUND IS statusCountSideTextWidest AND IT IS APPLIED TO THE WHOLE PART —
	// never to the fraction, and never by dropping digits. A whole part wider than the
	// ladder's top rung can state is a value past the ladder's own units: it is printed
	// ROUNDED in that unit, so the approximation names the magnitude it was rounded to.
	// The rounding is what keeps the digits finite; clipping them instead would print a
	// number the counter never reported.
	if countDecimalDigits(whole) > maxDigits {
		// Past the widest unit the ladder has, and with no rung above to move to: a count
		// beyond the top unit's own range. The value is ROUNDED to the digits a rung may
		// print and carries the top unit, so the approximation still states its magnitude
		// (a trillion-fold count prints "10G", not a 13-digit integer). Rounding, never
		// clipping — a clipped prefix would print a number the counter never reported,
		// which is the one thing this row's honesty rule forbids.
		div := 1
		for i := 0; i < countDecimalDigits(whole)-maxDigits; i++ {
			div *= 10
		}
		return newBoundedCount(leadingDigits((whole+div/2)/div, 0)+unit, unit)
	}
	if frac == 0 {
		// A whole count, or one whose tenth rounds away: printed whole — a trailing ".0"
		// would spend a cell saying nothing.
		return newBoundedCount(leadingDigits(whole, 0)+unit, unit)
	}
	return newBoundedCount(leadingDigits(whole, 0)+
		statusCountFractionSep+leadingDigits(frac, 0)+unit, unit)
}

// countRungSplit splits a count into the whole and fractional parts it prints on the
// rung whose scale is given, the fraction scaled out of statusCountFractionScale and
// ROUNDED half away from zero. It is the ONE place a count is scaled to a unit, so the
// whole part and the fraction can never be rounded by different rules (Rule 14) — the
// defect being a fraction that truncated while the whole part did not, which rounds a
// count DOWN and reports it as if it were exact.
//
// A fraction that rounds up to the full scale (statusCountFractionScale) is returned
// as it is: the caller owns the carry, because only the caller knows which unit the
// carry should be expressed in.
func countRungSplit(n, scale int) (whole, frac int) {
	if scale <= statusCountUnits[0].scale {
		return n, 0
	}
	rest := n % scale
	return n / scale, (rest*statusCountFractionScale + scale/2) / scale
}

func newBoundedCount(text, unit string) boundedCount {
	return boundedCount{text: text, cells: statusFieldCells(text), unit: unit}
}

// statusCountFractionSep separates a count's whole part from its fraction
// ("166.2k"): the row's own decimal point, in the form the counts on this row
// already use (formatCount prints "%.1fk"). One definition, so a change of
// punctuation carries to the bounded form with it.
const statusCountFractionSep = "."

// statusCountDigitsBuf is the buffer leadingDigits renders into. It is sized for the
// widest decimal spelling an int can have — a 64-bit int reaches 19 digits, plus one
// for the arithmetic's own headroom — and NOT for the cap: leadingDigits also renders
// UNLIMITED numbers (countDecimalDigits measures an unbounded value with it, and the
// reservation's arithmetic is stated against that measurement), so a buffer sized to
// the cap would truncate the very number the geometry is derived from.
const statusCountDigitsBuf = 20

// boundedCountFits reports whether a bounded rendering fits a cell budget — the
// predicate a caller consults so that "does this form fit here" is answered by
// MEASURING the form rather than by assuming its bound.
//
// It is defined over boundedCount.cells, the same quantity the reservation is
// derived from (Rule 14), so the check and the geometry cannot disagree.
func boundedCountFits(rect boundedCount, budget int) bool {
	return rect.cells <= budget
}
