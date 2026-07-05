# Persona battery — carrier voice model (8B, checkpoint-900)

Base `unsloth/Qwen3-8B` + the FidoNet voice LoRA. Same prompt scaffold for every
persona (`system` = handle + style + echo, `user` = thread context), temperature
0.8, no repetition penalty. The question isn't "is it BBS-ish" — it's **does each
persona sound like a different person.**

Verdict: **the voice transfer clearly worked.** The model learned period register
*and* the FidoNet reply grammar (the `On <date>, X wrote to Y` quote headers are
straight out of the training data — we never prompted for them). Personas are
distinguishable. Two real warts, both expected for an 8B QLoRA at ~1000 steps with
no inference-time guards: **repetition loops** and occasional **rambling**. Fixes
are inference-side (repetition penalty, `no_repeat_ngram_size`, tighter max tokens)
plus possibly a light dedup/length filter on the data — not a retrain-from-scratch.

---

### warez_wolf → CrustyRon · "Re: your ratio"  ✅ in-voice
> I think that the "loss of access" part is a bit harsh. I mean, I have downloaded
> over 50 megabytes from here, and I'm sure I haven't even scratched the surface of
> what's available. I'll be glad to fix the ratio if you'll please let me continue
> to use the board for file sharing. Thanks.

Wheedling leech energy — negotiating to keep leeching. On-character.

### CrustyRon → l1ttl3h4x0r · "Re: how do i hack"  ✅ in-voice
> On (18 Aug 96) l1ttl3h4x0r wrote to All...
> This is not a good idea. Your school probably has a firewall... It is a good idea
> to be able to use your school's computer to access the Internet, but it is not a
> good idea to be able to hack the school's internal network.

Condescending sysop lecturing the kid. Learned the quote-header format.

### kitkat_16 → ALL · "anyone here NOT a nerd??"  ✅✅ best output
> hey, i'm just like you. i'm 16. i'm a girl. i don't do homework... i care about
> boys. i care about the boys i see at school and i care about the boys i see at
> the mall... i'm 16. i'm a girl. i'm not a nerd.

Nails the teen-girl register — lowercase, boy-obsessed, dismissive of school.
*Completely* different voice from CrustyRon. This is the persona-conditioning working.

### Phr34k → Dr_DOS · "Re: red boxing dead?"  ✅ in-voice
> On 20 Feb 98 20:35:00, Dr_DOS said the following to Phr34k... i have one that
> works on a digital phone... it is called a "spike"... i can not tell you how it
> works but it works and works well.

Menace-via-specifics phreak — vague, faintly threatening, name-drops a device.

### Seraphine → NightOwl · "Re: the midnight crew"  ⚠️ thin
> On 20 Feb 98 13:21, NightOwl said to Seraphine: That's why it's best to post in
> an echo that isn't private...

Correct format, but generic — the "gossip schemer" flavor didn't land. Short/flat.

### l1ttl3h4x0r → CrustyRon · "Re: how do i hack"  ⚠️ repetition loop
> i have the manual, but i dont get the part about how to hack
> anyone, anyone, anyone, anyone, anyone, anyone... [loops]

Starts perfectly in-character (cocky kid who has the manual but can't read it),
then degenerates into a token loop. Classic small-model failure; an inference-time
repetition penalty kills it.

### Dr_DOS → warez_wolf · "Re: himem.sys"  ✅ in-voice
> In a message to All, WAREZ_WOLF wrote: If you have more than 1 meg, you need to
> use HIMEM.SYS. If you have less, you need to use EMS.SYS (or, better yet, upgrade
> your memory).

Pedantic, correct, faintly superior. Peak Dr_DOS.

---

**5/7 clean, 1 thin, 1 looped.** For a first end-to-end fine-tune trained for ~$0.77,
that's a working voice tier. Next iteration: inference-time repetition guards, then
maybe more steps on a lightly-filtered dataset for the weaker personas.
