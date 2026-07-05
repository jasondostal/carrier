"""Persona battery for the carrier voice model (8B). Loads base Qwen3-8B + the
trained LoRA and generates a reply for each of several carrier personas across
varied thread contexts.

The test isn't "is it BBS-ish" — it's "does warez_wolf sound DIFFERENT from
kitkat_16", i.e. did the voice tier stay persona-conditioned instead of
collapsing into one generic 'BBS guy' voice.

Run on the pod after training:
    BASE_MODEL=unsloth/Qwen3-8B ADAPTER=/workspace/carrier-voice-8b-adapter \
      python test_personas_8b.py
"""
import os
import torch
from unsloth import FastLanguageModel
from peft import PeftModel

BASE = os.environ.get("BASE_MODEL", "unsloth/Qwen3-8B")
# point at the final adapter, or a checkpoint dir (…/carrier-voice-8b/checkpoint-NNN)
ADAPTER = os.environ.get("ADAPTER", "/workspace/carrier-voice-8b-adapter")

model, tok = FastLanguageModel.from_pretrained(
    model_name=BASE, max_seq_length=1024, load_in_4bit=True, dtype=None)
model = PeftModel.from_pretrained(model, ADAPTER)
FastLanguageModel.for_inference(model)

# (handle, style hint baked into system, echo, to, subject, quoted)
CASES = [
    ("warez_wolf", "terse ratio-obsessed file leech, allergic to small talk", "Files",
     "CrustyRon", "Re: your ratio",
     "CR> your download/upload ratio is a crime against this board. 47:1. fix it\nCR> or lose access. this isn't a public library, leech."),
    ("CrustyRon", "52yo BOFH grognard sysop, condescending, netiquette zealot", "General",
     "l1ttl3h4x0r", "Re: how do i hack",
     "LH> yo can someone tell me how to hack the school computer lol i wanna\nLH> change my grades. whats the best program for it"),
    ("kitkat_16", "16, avoiding homework, baffled by the nerds, boy-crazy", "General",
     "ALL", "anyone here NOT a nerd??",
     "(no quoted text — she's starting a thread)"),
    ("Phr34k", "menace-via-specifics phone phreak, quietly threatening", "Phreaking",
     "Dr_DOS", "Re: red boxing dead?",
     "DD> red boxing has been dead since the payphone networks went digital.\nDD> you kids romanticize a technique that stopped working in 1996."),
    ("Seraphine", "drama/romance schemer, runs the gossip, honeyed and cutting", "General",
     "NightOwl", "Re: the midnight crew",
     "NO> i saw what you posted about me and betty in the sysop conference.\nNO> that was supposed to be private. how did you even see that"),
    ("l1ttl3h4x0r", "cocky teen script kiddie, all bravado, easily owned", "General",
     "CrustyRon", "Re: how do i hack",
     "CR> Back in my day we RTFM'd instead of begging on message bases. The\nCR> answer to your every question is a manual you refuse to read, child."),
    ("Dr_DOS", "insufferable manual-citing pedant, corrects everyone", "Retro",
     "warez_wolf", "Re: himem.sys",
     "WW> cant get doom to run out of memory. someone said load himem. how"),
]

TMPL = ("You are {h}, a caller on a 1990s BBS posting in the {echo} message echo. "
        "Write like a real BBS user of the era — plain, direct, in your own voice, "
        "no modern polish. You are: {style}.")

for h, style, echo, to, subj, quoted in CASES:
    system = TMPL.format(h=h, style=style, echo=echo)
    user = f"You are replying to {to} about \"{subj}\"."
    if not quoted.startswith("(no"):
        user += f"\n\nThey wrote:\n{quoted}"
    user += "\n\nWrite your reply."
    msgs = [{"role": "system", "content": system}, {"role": "user", "content": user}]
    try:
        ids = tok.apply_chat_template(msgs, add_generation_prompt=True,
                                      enable_thinking=False, return_tensors="pt").to("cuda")
    except TypeError:
        ids = tok.apply_chat_template(msgs, add_generation_prompt=True, return_tensors="pt").to("cuda")
    out = model.generate(input_ids=ids, max_new_tokens=140, temperature=0.8,
                         top_p=0.9, do_sample=True)
    reply = tok.decode(out[0][ids.shape[1]:], skip_special_tokens=True).strip()
    print("\n" + "=" * 70)
    print(f"[{h}]  ->{to}  re: {subj}")
    print("-" * 70)
    print(reply)

print("\n" + "=" * 70 + "\nPERSONA BATTERY DONE")
