package ui

// The words a Wake agent shows while it works. Wake's own, written for Wake.
//
// # Why they are ours
//
// A working spinner's word list is a creative compilation - the selection and
// the arrangement are the work, which is the thing compilation copyright is
// for - so Wake does not borrow Claude Code's. Colours are numbers and a
// spinner is six glyphs; a list of authored words is not.
//
// So these are written for Wake, and the file says so because the provenance
// is the point. Do not "improve" this list by sampling Claude's.
//
// # Why it is this long
//
// 205 words, and the length is a requirement rather than enthusiasm: Wake is
// built for 15-30 agents at once, and every one of them with a turn in flight
// draws a word. A short pool puts the same word on screen three times and
// reads as a bug in the renderer. `TestHeartbeatPoolIsWellFormed` holds the
// floor at 180.
//
// # Why four of them are plain
//
// Thinking, Working, Computing and Processing are also in Claude's list, and
// they stay. They are the plainest English available for what a process is
// doing - a word nobody authored and nobody owns - and a pool of 206 jokes
// with no straight word in it is a pool that never lets the line be plain.
//
// The rest lean on what Wake is: a wake is what a ship leaves behind, so the
// nautical run is the product's own joke, and the dawn run is its name's.
var heartbeatWords = []string{
	"Alarming", "Ambling", "Anchoring", "Annealing", "Assembling", "Awakening",
	"Ballasting", "Ballyhooing", "Bamboozling", "Batching", "Beachcombing", "Beavering",
	"Becalming", "Blinking", "Bolting", "Braiding", "Brouhahaing", "Brunching", "Bubbling",
	"Bumbling", "Bundling", "Buoying", "Burbling", "Buttering",
	"Caffeinating", "Calibrating", "Cartwheeling", "Casting", "Catalysing", "Caterwauling",
	"Centrifuging", "Charting", "Chirping", "Chiselling", "Chortling", "Chunking", "Clamping",
	"Cockcrowing", "Codswalloping", "Collywobbling", "Computing", "Constellating", "Convening",
	"Cooing", "Corralling", "Cranking", "Cresting",
	"Dawdling", "Dawning", "Daybreaking", "Dealing", "Decanting", "Dispatching", "Distilling",
	"Docking", "Dredging", "Drifting",
	"Echoing", "Eclipsing", "Effervescing",
	"Faffing", "Fanning", "Fathoming", "Ferreting", "Ferrying", "Filing", "Filtering", "Firing",
	"Fizzing", "Flickering", "Fluffing", "Foaming", "Foraging", "Fossicking", "Frothing",
	"Fumbling", "Futzing",
	"Galumphing", "Galvanising", "Gearing", "Gleaming", "Gleaning", "Glimmering", "Glinting",
	"Gobbledygooking", "Greasing",
	"Hammering", "Harum-scarumming", "Helming", "Helter-skeltering", "Hoisting", "Hootenannying",
	"Hornswoggling", "Hubbubbing",
	"Jamboreeing", "Jamming", "Jibing",
	"Kerfuffling", "Kettling", "Kibitzing", "Kindling", "Knitting", "Kvetching",
	"Lacing", "Larking", "Lathing", "Levering", "Loafing",
	"Magnetising", "Magpieing", "Malarkeying", "Marshalling", "Mooching", "Mooring",
	"Navigating", "Netting", "Noodging",
	"Oiling", "Oscillating",
	"Paddling", "Pinging", "Pirouetting", "Plodding", "Polling", "Polymerising", "Poppycocking",
	"Portaging", "Pottering", "Preening", "Priming", "Processing",
	"Quenching", "Queueing",
	"Rallying", "Ratcheting", "Rekindling", "Relaying", "Relighting", "Resonating",
	"Reverberating", "Rigging", "Rigmaroling", "Rising", "Riveting", "Rostering", "Rousing",
	"Rowing", "Ruffling", "Rummaging",
	"Sailing", "Sanding", "Sauntering", "Scavenging", "Schmoozing", "Shepherding", "Shindigging",
	"Shuffling", "Sifting", "Skullduggering", "Snoozing", "Snorkelling", "Snuffling", "Soldering",
	"Somersaulting", "Sorting", "Sounding", "Sparking", "Splicing", "Squirrelling", "Stacking",
	"Steeping", "Stirring", "Stitching", "Stoking", "Stretching", "Strolling", "Stumbling",
	"Sunning", "Surfing",
	"Tacking", "Tallying", "Thinking", "Threading", "Titrating", "Toasting", "Torquing",
	"Traipsing", "Trawling", "Trilling", "Trimming", "Trudging", "Trundling", "Tumbling", "Tuning",
	"Unshuttering",
	"Vulcanising",
	"Waffling", "Warbling", "Weaving", "Welding", "Whiffling", "Winding", "Wingdinging",
	"Wombling", "Working",
}
