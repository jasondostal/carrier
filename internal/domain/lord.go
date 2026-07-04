package domain

import (
	"fmt"
	"math/rand"
)

// Legend of the Red Dragon, for real (enough): a door with actual stakes.
// Callers fight the forest, level up, buy gear, flirt with Violet at the Inn,
// and ambush each other on the road — and every outcome is a narrated event the
// sysop watches and the Daily News picks up. Each door action resolves ONE
// encounter, which fits the tick model; it's not turn-by-turn combat, but the
// numbers, deaths, level-ups, and rivalries are genuine and persist.

const forestFightsPerDay = 15

type gear struct {
	name string
	cost int
	pow  int
}

// Weapons at King Arthur's, cheapest first — the classic LORD ladder, trimmed.
var lordWeapons = []gear{
	{"Fists", 0, 6}, {"Dagger", 200, 12}, {"Short Sword", 1000, 30},
	{"Long Sword", 3000, 60}, {"Huge Axe", 10000, 120}, {"Bone Cruncher", 30000, 240},
	{"Twin Swords", 100000, 480}, {"Death Sword", 400000, 900},
}

// Armor at Abdul's, cheapest first.
var lordArmors = []gear{
	{"Coat", 0, 2}, {"Leather Vest", 200, 8}, {"Bronze Armor", 1000, 18},
	{"Iron Armor", 3000, 36}, {"Graphite Armor", 10000, 72}, {"Erdrick's Armor", 30000, 140},
	{"Armor of Death", 100000, 280}, {"Full Body Armor", 400000, 520},
}

// Forest beasts — scaled by the player's level; the names are just flavor.
var lordBeasts = []string{
	"a Gremlin", "an Evil Fairy", "a Small Thief", "a Bearon", "a Wild Boar",
	"an Orc", "a Skeleton Warrior", "a Crazed Merchant", "a Cave Troll", "a Dark Rider",
	"a Wraith", "a Rabid Griffin", "an Ugly Mutant", "the Ghost of a Sysop",
}

// LordPlayer is one caller's Red Dragon character. Persistent across sessions
// (and, with --persist, across runs).
type LordPlayer struct {
	Level      int
	Exp        int
	HP, MaxHP  int
	Gold       int
	WeaponIdx  int
	ArmorIdx   int
	Charm      int
	ForestLeft int    // forest fights remaining today
	Alive      bool   // dead players wait for the healer at dawn
	Married    string // spouse handle, or "Violet"
}

func newLordPlayer() *LordPlayer {
	return &LordPlayer{Level: 1, HP: 20, MaxHP: 20, Gold: 40, ForestLeft: forestFightsPerDay, Alive: true}
}

// Lord returns (creating on first play) a persona's Red Dragon character.
func (w *World) Lord(id string) *LordPlayer {
	if w.Lords == nil {
		w.Lords = map[string]*LordPlayer{}
	}
	lp, ok := w.Lords[id]
	if !ok {
		lp = newLordPlayer()
		w.Lords[id] = lp
	}
	return lp
}

// NewDay revives and re-supplies a character at the start of a LORD day.
func (lp *LordPlayer) NewDay() {
	lp.ForestLeft = forestFightsPerDay
	lp.Alive = true
	lp.HP = lp.MaxHP
}

// WeaponName / ArmorName are exported so the perception layer can show a caller
// what they're carrying without reaching into the gear tables.
func (lp *LordPlayer) WeaponName() string { return lordWeapons[lp.WeaponIdx].name }
func (lp *LordPlayer) ArmorName() string  { return lordArmors[lp.ArmorIdx].name }

func (lp *LordPlayer) power() int   { return lordWeapons[lp.WeaponIdx].pow + lp.Level*5 }
func (lp *LordPlayer) defense() int { return lordArmors[lp.ArmorIdx].pow + lp.Level*3 }

func expToLevel(level int) int { return level * level * 100 }

// Summary is the one-line character sheet shown to the caller each turn.
func (lp *LordPlayer) Summary() string {
	state := "alive"
	if !lp.Alive {
		state = "DEAD until dawn"
	}
	spouse := ""
	if lp.Married != "" {
		spouse = ", married to " + lp.Married
	}
	return fmt.Sprintf("level %d · %d/%d HP · %d gold · %s / %s · charm %d · %d forest fights left · %s%s",
		lp.Level, lp.HP, lp.MaxHP, lp.Gold, lp.WeaponName(), lp.ArmorName(), lp.Charm, lp.ForestLeft, state, spouse)
}

// Forest resolves one forest encounter. Returns a narrated line and whether it's
// notable enough for the Daily News (level-ups, deaths).
func (w *World) Forest(handle string, lp *LordPlayer, rng *rand.Rand) (string, bool) {
	if !lp.Alive {
		return fmt.Sprintf("%s is dead and can't fight until the healer revives them at dawn.", handle), false
	}
	if lp.ForestLeft <= 0 {
		return fmt.Sprintf("%s is out of forest fights for today.", handle), false
	}
	lp.ForestLeft--
	beast := lordBeasts[rng.Intn(len(lordBeasts))]
	beastStr := 10 + lp.Level*7 + rng.Intn(12+lp.Level*4)

	if lp.power()+rng.Intn(24) >= beastStr {
		gold := 12 + rng.Intn(20) + lp.Level*10
		exp := 6 + rng.Intn(10) + lp.Level*7
		lp.Gold += gold
		lp.Exp += exp
		if lp.Exp >= expToLevel(lp.Level) {
			lp.Exp = 0
			lp.Level++
			lp.MaxHP += 12
			lp.HP = lp.MaxHP
			return fmt.Sprintf("%s slew %s and ROSE TO LEVEL %d!", handle, beast, lp.Level), true
		}
		return fmt.Sprintf("%s cut down %s for %d gold and %d exp.", handle, beast, gold, exp), false
	}

	dmg := beastStr - lp.defense()
	if dmg < 1 {
		dmg = 1
	}
	lp.HP -= dmg
	if lp.HP <= 0 {
		lost := lp.Gold / 2
		lp.Gold -= lost
		lp.HP = 0
		lp.Alive = false
		return fmt.Sprintf("%s was KILLED by %s in the dark forest, dropping %d gold!", handle, beast, lost), true
	}
	return fmt.Sprintf("%s traded blows with %s and staggered back, wounded (%d/%d HP).", handle, beast, lp.HP, lp.MaxHP), false
}

// Inn is the flirt-with-Violet path; enough charm wins her hand.
func (w *World) Inn(handle string, lp *LordPlayer, rng *rand.Rand) (string, bool) {
	lp.Charm++
	if lp.Charm >= 10 && lp.Married == "" {
		lp.Married = "Violet"
		return fmt.Sprintf("%s WON VIOLET'S HEART and married her at the Inn!", handle), true
	}
	lines := []string{
		"%s bought Violet a drink at the Inn. She giggled and looked away.",
		"%s spent the evening flirting with Violet by the fire.",
		"%s tried a smooth line on Violet at the bar — mixed results.",
		"%s and Violet shared a quiet drink; charm is rising.",
	}
	return fmt.Sprintf(lines[rng.Intn(len(lines))], handle), false
}

// Shop upgrades the caller's weapon, then armor, to the next affordable tier.
func (w *World) Shop(handle string, lp *LordPlayer) (string, bool) {
	if lp.WeaponIdx+1 < len(lordWeapons) && lp.Gold >= lordWeapons[lp.WeaponIdx+1].cost {
		lp.WeaponIdx++
		g := lordWeapons[lp.WeaponIdx]
		lp.Gold -= g.cost
		return fmt.Sprintf("%s bought a %s at King Arthur's Weapons.", handle, g.name), false
	}
	if lp.ArmorIdx+1 < len(lordArmors) && lp.Gold >= lordArmors[lp.ArmorIdx+1].cost {
		lp.ArmorIdx++
		g := lordArmors[lp.ArmorIdx]
		lp.Gold -= g.cost
		return fmt.Sprintf("%s suited up in %s at Abdul's Armour.", handle, g.name), false
	}
	return fmt.Sprintf("%s browsed the shops but couldn't afford an upgrade (%d gold).", handle, lp.Gold), false
}

// Attack is player-vs-player on the road. def may be nil (no such player).
func (w *World) Attack(attacker string, atk *LordPlayer, defender string, def *LordPlayer, rng *rand.Rand) (string, bool) {
	if !atk.Alive {
		return fmt.Sprintf("%s is too dead to pick a fight.", attacker), false
	}
	if def == nil || defender == "" {
		return fmt.Sprintf("%s went looking for a fight but found no one by that name.", attacker), false
	}
	if defender == attacker {
		return fmt.Sprintf("%s shadow-boxed for a while. Weird.", attacker), false
	}
	if !def.Alive {
		return fmt.Sprintf("%s went after %s, but they're already dead today.", attacker, defender), false
	}

	if atk.power()+atk.Level*4+rng.Intn(26) >= def.defense()+def.Level*4+rng.Intn(26) {
		loot := def.Gold/2 + 25
		if def.Gold < loot {
			loot = def.Gold + 25
		}
		def.Gold -= (loot - 25)
		if def.Gold < 0 {
			def.Gold = 0
		}
		atk.Gold += loot
		def.Alive = false
		def.HP = 0
		return fmt.Sprintf("%s AMBUSHED %s on the road and left them for dead, taking %d gold!", attacker, defender, loot), true
	}

	atk.HP -= 6 + rng.Intn(10)
	if atk.HP <= 0 {
		atk.HP = 0
		atk.Alive = false
		return fmt.Sprintf("%s picked a fight with %s and got themselves killed. Embarrassing.", attacker, defender), true
	}
	return fmt.Sprintf("%s swung at %s but got driven off, bloodied (%d/%d HP).", attacker, defender, atk.HP, atk.MaxHP), false
}
