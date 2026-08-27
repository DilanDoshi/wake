# Per-package coverage gate. Reads a Go coverage profile; fails if any package
# is below its floor.
#
# Per package, not on the total, because the total is a mean advertised as a
# floor. `rpc` and `notice` are 50 statements at 100%, and they hold the mean up
# well enough that a package can rot most of the way to zero before the number
# moves - so the gate is loudest exactly when it has least to say. A minimum
# fails when one package rots, which is when someone can still do something
# about it.
#
# Read from the profile rather than from `go test`'s stdout: the profile is a
# stable format, while the stdout line moves around (`(cached)`, `[no test
# files]`, `FAIL`) and parsing it is how a gate ends up measuring nothing.
#
# Variables, all required: min, xpkg, xmin. See the Makefile.
#
#   awk -v min=80 -v xpkg=<pkg> -v xmin=76 -f scripts/covergate.awk coverage.out

# Line 1 is "mode: set|count|atomic".
NR == 1 { next }

# Every other line is "<file>:<start>.<col>,<end>.<col> <statements> <count>".
# The package is the file's directory.
{
  split($1, field, ":")
  path = field[1]
  n = split(path, seg, "/")
  pkg = seg[1]
  for (i = 2; i < n; i++) pkg = pkg "/" seg[i]

  if (!(pkg in seen)) { seen[pkg] = 1; order[++npkg] = pkg }
  total[pkg] += $2
  if ($3 > 0) covered[pkg] += $2
}

END {
  # The floors themselves, before anything is compared against them. An unset
  # or renamed COVER_MIN arrives here as the empty string, which compares as 0
  # and passes every package - a gate printing "gate 0%" and exiting 0. Today
  # that is caught only by accident, because the exemption ratchet fires first;
  # delete the exemption as it is designed to be deleted and the accident goes
  # with it.
  if (min == "" || min + 0 <= 0) {
    printf "min is %s: this gate would pass every package.\n", (min == "" ? "unset" : "\"" min "\"")
    print  "Pass -v min=$(COVER_MIN) with a positive value."
    exit 1
  }
  if (xpkg != "" && (xmin == "" || xmin + 0 <= 0)) {
    printf "an exemption is set for %s but xmin is %s, which exempts it from everything.\n", \
      xpkg, (xmin == "" ? "unset" : "\"" xmin "\"")
    print  "Pass -v xmin=$(COVER_EXEMPT_MIN) with a positive value, or drop the exemption."
    exit 1
  }

  # A gate that reports green over an empty profile is the failure this whole
  # file exists to prevent, one level up.
  if (npkg == 0) {
    print "coverage profile named no packages: this gate is checking nothing."
    print "Something upstream produced an empty profile - fix that rather than this."
    exit 1
  }

  # An exemption for a package that is not here has been silently doing nothing
  # since whenever it was renamed.
  if (xpkg != "" && !(xpkg in seen)) {
    printf "the coverage exemption names %s, which is not in the profile.\n", xpkg
    print  "It is exempting nothing. Fix the name, or delete the exemption."
    exit 1
  }

  bad = 0
  for (i = 1; i <= npkg; i++) {
    p = order[i]
    pct = total[p] ? covered[p] * 100 / total[p] : 0
    floor = (p == xpkg) ? xmin : min
    mark = "ok "
    if (pct + 0 < floor + 0) { mark = "LOW"; bad++ }
    printf "  %s %-44s %5.1f%%  (floor %d%%)%s\n", \
      mark, p, pct, floor, (p == xpkg ? "  exempt" : "")
  }

  # The exemption is meant to be temporary, and the only reliable way to make a
  # temporary thing temporary is to fail when it stops being needed.
  if (xpkg in seen) {
    xpct = total[xpkg] ? covered[xpkg] * 100 / total[xpkg] : 0
    if (xpct + 0 >= min + 0) {
      printf "\n%s is at %.1f%% and has outgrown its exemption.\n", xpkg, xpct
      print  "Delete COVER_EXEMPT_PKG and COVER_EXEMPT_MIN from the Makefile: an exemption"
      print  "nobody removes is how a temporary floor becomes the permanent one."
      exit 1
    }
  }

  if (bad > 0) {
    printf "\n%d package(s) below the floor.\n", bad
    print  "This gate is a minimum, not a mean: raising an already-covered package does not"
    print  "fix this, and the total meeting 80% would not have reported it at all."
    exit 1
  }

  printf "\nevery package meets its floor (gate %d%%)\n", min
}
