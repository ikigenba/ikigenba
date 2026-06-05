# The Greased Lightning Ledger: Talking to Your Books

*This ledger has no screen. There is no app to open, no spreadsheet to balance, no row of green cells that becomes load-bearing for your whole business until the day it betrays you. You run the entire thing by talking to your assistant in plain English. You describe what happened — "Moe paid cash for the $450 transmission job" — and the books take care of themselves.*

*What follows is one continuous story: you, Homer Simpson, opening Greased Lightning Auto with ten grand of your own money and a loan, and keeping its books for a whole year — the first cash sale, an invoice that lingers, a fat-fingered utility bill you fix without ever lying about it, a bank reconciliation, and a year-end close. Ride along start to finish, or jump to "Say It Your Way" and the field reference at the back whenever you just need to look something up.*

## Table of Contents

1. [Talking to Your Ledger](#1-talking-to-your-ledger)
2. [Meet the Cast](#2-meet-the-cast)
3. [What Your Ledger Remembers](#3-what-your-ledger-remembers)
4. [What You Can Ask For](#4-what-you-can-ask-for)
5. [Day One — Opening the Shop](#5-day-one--opening-the-shop)
6. [The Daily Rhythm — Sales, Invoices & Bills](#6-the-daily-rhythm--sales-invoices--bills)
7. [End of Month — Reconcile & Report](#7-end-of-month--reconcile--report)
8. [End of Year — Closing the Books](#8-end-of-year--closing-the-books)
9. [Say It Your Way](#9-say-it-your-way)
10. [Cheatsheet, Field Reference, Gotchas & FAQ](#10-cheatsheet-field-reference-gotchas--faq)

---

## 1. Talking to Your Ledger

Here is the big idea, and it is genuinely the whole trick: **you don't learn double-entry bookkeeping. You just talk to your assistant, and your assistant knows the books.**

There is no app to open, no form to fill out, no ledger paper with two columns you have to keep level by hand. You connect the ledger to Claude once, and from then on you say things like *"Moe paid cash for the $450 transmission job"* and the right entries appear, balanced to the penny. Your assistant works out which accounts to touch, which side each one goes on, and how to make the two halves match. You never have to.

You also never have to speak the bookkeeper's dialect. You will **never type an account name, a debit, a credit, a plus or minus sign, or a figure in cents.** Say "$450" and the ledger stores the exact amount. Say "Moe paid cash" and the assistant knows the money landed in the till, not the bank. Say "bill Mr. Burns for the engine overhaul" and it knows that means money is now owed *to you* — a thing with a precise home in the books — and it files it there. You talk like a person who fixes cars; the assistant handles the accountancy.

> **The one promise this whole guide rests on.** You describe *what happened*. The assistant picks the accounts, assigns the debit/credit signs, makes the two sides balance, and does the math. You bring the story in your own words; it maps that story onto the books. That's the deal, start to finish.

And there is no magic phrase to memorize. "Moe paid cash for the transmission," "log a $450 cash sale to Moe," and "we did Moe's transmission, he paid four-fifty in cash" all land in exactly the same place. There is just what you mean, said however you would naturally say it. We will hammer this so hard you'll get sick of it — but it's the single most freeing fact about running your books this way.

You will also never juggle a transaction number. Every entry the ledger keeps does have a unique ID under the hood — an opaque string the system generates — but you will never see one or type one. You say "that wrong utilities bill from the 30th," and your assistant keeps track of which exact entry that is.

### It Is a Real Set of Books, Not a Checkbook

This is not a running tally of your bank balance. It is **double-entry bookkeeping** — the same discipline a real accountant uses — which means every single thing that happens to your money is recorded as a small, balanced story with (at least) two sides: where the money came *from* and where it went *to*. Buy a lift with bank money and the books note both that you have a lift now *and* that the bank account dropped. Bill a customer and the books note both the income you earned *and* the promise that they'll pay. Nothing happens in only one place, because in the real world money never does.

That sounds like a lot of bookkeeping. It is — and you will do none of it. You say "we bought a hydraulic lift for eight thousand out of the bank account," and the assistant writes both sides. The discipline is real; the labor is the robot's.

### What "Talking to It" Actually Looks Like

You speak English. The assistant translates into balanced entries. A few representative trades:

- **You:** "Moe paid cash for the $450 transmission job."
  **Assistant (behind the curtain):** records one balanced transaction — cash in the drawer goes up $450, labor income goes up $450 — the two sides offsetting so the books stay level.
  **You see:** "Logged Moe's $450 cash transmission job."

- **You:** "What did the shop make and spend in June?"
  **Assistant:** reads the income accounts and the expense accounts for June and reports the difference in plain dollars — "June revenue $2,550, expenses $3,770, so you're $1,220 in the red for the month."
  **You see:** a plain-dollar P&L, no jargon.

- **You:** "That utilities bill was wrong — I typed fifteen hundred, it was a hundred fifty."
  **Assistant:** doesn't reach in and scribble over the old entry. It posts a reversal that cancels the mistake, then records the correct $150 — leaving an honest trail of all three.

Notice you never said an account path, a cents figure, a sign, or the words "debit" and "credit." You said what *happened*. That is the entire user interface.

### It Keeps You Honest

A good set of books has one job above all others: to be trustworthy. So a couple of its habits are strict on purpose, and they're both gifts:

- **You never erase; you reverse.** The journal — the running record of every transaction — is permanent. There is no "edit this entry" and no "delete this entry." If you record something wrong, the fix is to post a mirror-image entry that cancels it out, then record it correctly. The mistake, its cancellation, and the correction all stay on the books. It feels like extra steps until the first time you're glad nobody — not even you — could quietly rewrite what the books said back in June. (You'll watch this exact dance with a fat-fingered utility bill in the story.)
- **The books always balance — and that's a free correctness check.** Because every entry has two equal-and-opposite sides, the grand total of everything, across every account, is always exactly zero. If it weren't, something would be wrong. You'll never run that check yourself, but it's quietly running under everything, which is why you can trust the number when the assistant tells you you're $1,220 in the red.

You'll see all of this in action later; for now, just know the books can't be cooked, even by accident.

### Your "Is This Thing On?" Test

The very first time you connect, ask your assistant something like *"check that you're connected to my ledger and tell me who I am."* It will run the one move whose entire job is to report back the account it sees you as — no input needed. If that comes back with your email, the whole chain — your assistant, the connection, the login, the service — is wired up correctly and you are ready to open the books. If it doesn't, that's your signal to fix the connection before you start trusting it with real money.

That is the whole mental model: plain English on your end, a proper set of double-entry books on the other, kept balanced and honest for you. The rest of this guide follows one shop — Greased Lightning Auto — through its first year, opening the books, working the daily rhythm of sales and bills, closing the month, and closing the year. You'll meet the cast next.

**How to read this guide:** skim it straight through to ride along with the story, or jump to "Say It Your Way" and the cheatsheet at the back whenever you just need to look something up.

---

## 2. Meet the Cast

Most bookkeeping tutorials introduce you to "Account 1010" and "Customer B," and your eyes glaze over before the first journal entry. We're going to do this differently: you'll learn these books the way you'll actually use them — by running one real (well, real-ish) shop through a whole year, from the day it opens to the day you close out the year.

The setting is **Greased Lightning Auto**, a small independent auto-repair shop in Springfield, and you own it. There's one set of books for the whole shop. Let's do introductions.

### You: Homer Simpson, Owner

**You're Homer** — a genuinely good mechanic and, by your own cheerful admission, hopeless with money. (The Mr. Plow empire. The grease-reselling scheme. We remember.) The beautiful part is that none of that matters here, because you are not going to *do* any accounting. You're going to *describe your day* — "Lenny and Carl got paid," "bought parts on the shop card," "Mr. Burns finally paid his bill" — and the assistant keeps the books. You teach the most important lesson in this whole guide, just by being yourself: **you say what happened; the ledger does the precise part.**

### The Mechanics: Lenny & Carl

**Lenny and Carl** turn the wrenches and, more to the point, expect to be paid for it. They teach **wages** — money that leaves the shop as an *expense*, the cost of getting the work done. Every payday is a little lesson in where the money goes.

### The Cash Customer: Moe Szyslak

**Moe** comes in for a transmission, it's **$450**, and he pays **cash** — bills, into the till. Moe teaches the simplest transaction there is: **a cash sale, money in.** (He's also the user's own example, so he's earned his place.) Watch Moe first; every other sale is a variation on him.

### The Prompt Payer: Ned Flanders

**Ned** is the customer every shop dreams of: you do the work, you send him a bill, and he *pays it* — promptly, with a "thanks a diddly-doo." Ned teaches an **invoice that gets paid and closes quickly**: the money is owed to you for a few days, then it's in the bank and the matter is settled. He's the gentle version of the lesson Mr. Burns is about to teach the hard way.

### The Slow Payer: Mr. Burns

**Mr. Burns** is fabulously wealthy and famously, gloriously reluctant to part with a dime. He buys a big **engine overhaul on account** — meaning he'll pay later, theoretically — and then takes his sweet time. His unpaid bill is the **receivable we follow cradle-to-grave**: born in June, still sitting open at month-end (he's the reason the month-end "who owes me?" report has a number in it), and finally paid in July. Burns teaches the full life of an invoice, and why "I earned it" and "I've got it" are two very different things.

### The Parts Supplier: Crazy Vaclav

**Crazy Vaclav** ("place of automobiles") sells you parts. Sometimes you buy them **on account** — he sends a bill, you owe him, you pay it later — which teaches the mirror image of a customer invoice: **money you owe a vendor**, and what it looks like when you pay it off. Other times you grab parts on the **shop credit card**, which teaches yet another flavor of "owe." Vaclav is your tour guide to the *owing* side of the books.

### The Bank: Springfield Savings

**Springfield Savings** put up the **startup loan** that helped open the doors, and every month it mails a **statement** — its official record of what actually moved through your checking account. Matching your books against that statement is called **reconciling**, and Springfield Savings is your partner in it. It teaches the loan (money you owe long-term) and the monthly ritual of proving your books agree with the bank's.

### The Cast, at a Glance

| Who | Role in the story | What they teach |
|---|---|---|
| **You / Homer** | The owner keeping the books by talking | You describe what happened; the ledger does the precise part |
| **Lenny & Carl** | The two mechanics on payroll | Wages — money out as an expense |
| **Moe Szyslak** | The cash customer ($450 transmission) | The simplest sale: cash in |
| **Ned Flanders** | The prompt-paying customer | An invoice that opens and closes fast (a receivable) |
| **Mr. Burns** | The wealthy, slow-paying customer | The receivable cradle-to-grave: billed → outstanding → paid → closed |
| **Crazy Vaclav** | The parts supplier | A vendor bill you owe (a payable), and the credit-card parts run |
| **Springfield Savings** | The bank | The startup loan, and the monthly reconciliation |

### The Two Through-Lines We'll Follow

Two storylines thread through the whole guide, and watching them play out beginning-to-end is the fastest way to understand the books:

- **Mr. Burns's invoice, cradle to grave.** You bill him for the engine overhaul (money is now owed to you). The bill sits open while he dithers — straight through the end of June, so it's still on the "who owes me?" list at month-end. Then in July he finally pays, and the matter closes. One invoice, four chapters: **billed → outstanding → paid → closed.**
- **A payment's confirmation journey.** Every entry tied to the bank starts life as `pending` — recorded, but not yet confirmed against the bank's own statement. When it shows up on the statement, it's `cleared`. And once you've matched the whole statement and locked it in, it's `reconciled`. Watch a single deposit ride that ladder: **pending → cleared → reconciled.** (And watch one rent check stubbornly stay `pending` because it hasn't hit the bank yet — that gap is the whole point of reconciling.)

### The Cadence — Day One, Daily, Month, Year

Real books have a rhythm, and this guide follows it exactly, because that's how you'll actually live in them:

1. **Day One** — the one-time setup: you put your own money in, take the loan, buy the big equipment, and watch the chart of accounts spring into existence from nothing.
2. **The daily rhythm** — the bread and butter: cash sales, invoices, vendor bills, the credit card, payroll, rent. Most of your bookkeeping life is right here, and it's all the same simple move repeated.
3. **End of month** — close the month out: reconcile against the bank statement, then pull the reports (who owes you, what you made and spent, where you stand).
4. **End of year** — close the books: sweep the year's profit into the owner's stake so the books reflect that you're actually worth more than when you started.

### The Year, in One Breath

Here's the whole movie, so you can see where the next chapters head:

Homer opens Greased Lightning with **$10,000** of his own money and a **$15,000** loan, and buys an **$8,000** lift. Moe pays cash for a transmission. Ned gets billed and pays right away; Mr. Burns gets billed and *doesn't*. Parts come in from Vaclav — some on account, some on the card. Lenny and Carl get paid, rent gets paid, a utility bill gets fat-fingered and honestly corrected. At month-end you reconcile against Springfield Savings and discover June was a **$1,220 loss** — which is fine, it's month one, you bought a lift. Burns finally pays in July. And come December 31, after a year that turned a **$21,000 profit**, you close the books, and the shop's worth on paper finally tells the truth.

That's it. That's bookkeeping: a balanced, honest memory of every dollar that moved, from "open for business" to "what a year." Everyone you just met is about to walk through it one transaction at a time — and you'll run the whole thing just by talking. Let's open the shop.

---

## 3. What Your Ledger Remembers

Here's the thing nobody tells you about a set of books: it isn't a calculator, it's a **memory** — a permanent, balanced record of everything that ever happened to your money. You don't "do accounting." You tell your assistant something true about your shop, and it remembers — so that at month-end, when you ask "who owes me money?" or "did I make a profit?", the answer is already there, exact to the penny.

And here's the surprise: there is really only **one kind of thing** your ledger writes down. Everything else is just *reading* what's been written. Get that one thing, and the five buckets it sorts money into, and you understand the whole shape of your books.

Everything below is something you'd *say* to your assistant in plain English. You never type any of it. We're just showing you what's in there so you know what you can ask for.

### The One Thing It Writes Down: A Balanced Transaction

Your ledger remembers your shop as a stream of **transactions** — and a transaction is just *one thing that happened to your money, recorded with both sides showing.*

Every transaction has at least **two sides** (bookkeepers call them *postings*), because money always comes from somewhere and goes somewhere. Moe's $450 cash sale has two sides: the till goes up $450, and labor income goes up $450. Buying the lift has two sides: you own a lift now, and the bank account went down. The magic rule — the one that makes the books trustworthy — is that **the two sides always balance.** The amount coming from one place exactly equals the amount going to the other. Add up all the sides of any transaction and they cancel to **zero**.

> **The whole rule, in one line.** A transaction is two or more sides that sum to zero. Every dollar that appears on one side has to come off another. There's no such thing as money from nowhere — and the ledger simply won't let you record one that doesn't balance.

Now, the part you've been quietly dreading — debits and credits — and the good news about it:

In old-school bookkeeping, the "two sides" are called **debit** and **credit**, and generations of people have been tortured trying to remember which is which. Here's everything you need to know: **the two sides have to balance, and the assistant decides which side each account goes on.** You will never type the word "debit" or "credit." You will never type a plus or a minus. You'll say "Moe paid cash for the $450 job," and the assistant works out — silently, correctly — that the till is the debit side and the income is the credit side, and that they net to zero. The signs are real and they matter; they are also entirely the robot's job.

> **You never do the signs.** Think of every transaction as a tiny see-saw that has to sit level. You describe what happened; the assistant places the weights on each side to balance it. You don't even have to know there *are* two sides — though now you do.

There's one lovely shortcut worth meeting now, because it's the secret to why this never feels like work. When you describe a transaction, you usually only know *one* number — "Mr. Burns owes me $1,854." You don't separately know that it breaks into $1,200 of labor, $600 of parts, and $54 of sales tax… actually, sometimes you do, and sometimes you just know the total. Either way: **you say the numbers you know, and the assistant lets the last side balance itself.** Tell it the total and the parts, and the remaining side is whatever makes it level. We call this *the balancing side fills itself in*, and you'll see it everywhere.

### The Five Buckets

Every account in your books — and every account is just a labeled place money can sit — belongs to one of exactly **five buckets**. Learn these five and you've learned the whole filing system. In plain English, they are:

1. **Assets — what you *own*.** The good stuff. Your checking account, the cash in the till, the hydraulic lift, and money customers owe you (yes — a promise to pay you is something you own). When Moe's cash hits the till or you buy the lift, you're moving things around inside **Assets**.

2. **Liabilities — what you *owe*.** The other shoe. The startup loan, the balance on the shop credit card, a bill from Vaclav you haven't paid yet, and sales tax you collected from customers that really belongs to the state. Anything you'll have to pay *out* someday lives in **Liabilities**.

3. **Equity — your *stake*.** What's actually *yours* once you subtract what you owe from what you own. The money Homer put in to start the shop lives here, and so does accumulated profit. It's the honest answer to "if I sold everything and paid off everyone, what's left for me?"

4. **Income — what you *earn*.** Money the shop makes by doing its job: labor on a repair, parts sold to a customer. Every sale adds to **Income**. (Old hands call this "Revenue," and the ledger happily answers to that word too.)

5. **Expenses — what you *spend*.** The cost of doing business: wages, rent, utilities, the parts you buy to do the work. Every dollar that goes out to *run* the shop lands in **Expenses**.

That's the entire chart of accounts, conceptually: **own, owe, your stake, earn, spend.** Every transaction you'll ever record is just money moving between these five buckets — a sale moves money from Income into Assets, paying rent moves it from Assets into Expenses, and so on. Five buckets. That's the whole map.

> **One quiet bit of bookkeeping arithmetic, stated once and then handled for you.** These five buckets always relate the same way: what you *own* equals what you *owe* plus your *stake* (and your stake grows by what you earn and shrinks by what you spend). That's *why* every transaction balances — it's just keeping that equation true. You will never compute this. The assistant keeps it true on every single entry, automatically, forever.

### Accounts Appear When You Need Them

Here's a chore you will never do: **maintaining a chart of accounts.** In most accounting software, before you can record anything you have to go create the account first — "set up a new account called Assets:Bank:Checking" — like installing shelves before you're allowed to put anything on them.

Not here. Accounts are **emergent**: they spring into existence the first time you use them. The very first time you describe putting money in the bank, the checking account is born. The first time you bill Mr. Burns, an account for *what Burns owes you* appears, all by itself. The first time you pay rent, a rent expense account shows up. You never "create an account" as a step — you just describe what happened, and the assistant files it under the right name, inventing that name on the spot if it's new.

There is exactly **one guardrail**, and it's the thing that keeps your books from turning into chaos: every account has to live under one of the five buckets — `Assets`, `Liabilities`, `Equity`, `Income`, or `Expenses`. Below that top level, the assistant organizes things sensibly (separating *what Burns owes* from *what Flanders owes*, say), but you never have to think about any of it. You describe the shop; the filing system builds itself.

### The Confirmation States — Pending, Cleared, Reconciled

Recording that money moved and *confirming* it actually moved at the bank are two different things, and your books track the difference. Every side of a transaction carries a little status — its place on the confirmation ladder:

- **`pending`** — recorded, but not yet confirmed against an outside source. This is where everything starts. You wrote the check; it hasn't hit the bank yet.
- **`cleared`** — confirmed to have actually gone through. You saw it on the bank or credit-card statement; the money really moved.
- **`reconciled`** — matched against an official statement and locked in. This is the gold standard: your books and the bank's books agree, and you've sealed it.

You'll walk a deposit up this whole ladder during the month-end reconciliation — and you'll watch one rent check stay stuck on `pending` because it hasn't cleared the bank yet. That gap, between what your books say and what's actually cleared, is exactly what reconciling is *for*. The status is the only thing about an existing entry you're ever allowed to change — and even then, you change only the *status*, never an amount, an account, or a date.

### Immutability — You Reverse, You Never Erase

This is the one rule that makes a set of books worth trusting, so meet it now: **the journal never changes. No edits, no deletes.** Once a transaction is written, it's written.

That sounds alarming — *what if I make a mistake?* — but the answer is elegant, and it's how real accountants have worked for centuries. To fix a mistake, you **reverse** it: the assistant posts a mirror-image transaction, every side flipped, that perfectly cancels the wrong one out. Then you record the right version. The mistake, its cancellation, and the correction all stay on the books, in order — an honest trail of what happened and how you fixed it.

> **You reverse; you never erase.** A book you could quietly rewrite isn't a record — it's a rumor with totals. So when you fat-finger a utility bill (and in the story, you will), you don't scrub it away. You cancel it with a reversal and record it right, and anyone reading the books later can see exactly what occurred. You'll just say "that was wrong, fix it" — the assistant does the reversal-and-re-record dance for you.

The lone, narrow exception is that confirmation status above: you *are* allowed to nudge a posting from `pending` to `cleared` to `reconciled`, because that's not rewriting history — it's recording that the outside world caught up to it.

### One Last Reassurance

You will never need to memorize an account name, which bucket something goes in, whether a thing is a debit or a credit, or how to make two sides balance. That's your assistant's job — all of it. You speak; it figures out which accounts you meant, which side each one goes on, and what makes the transaction sum to zero.

This section exists so that when the answer comes back, you *recognize* it — so "you're $1,220 in the red for June, but Burns still owes you $1,854" reads like a sentence about your shop, not a database dump.

One thing it writes down: a balanced transaction. Five buckets it sorts money into. A small, fixed handful of ways to talk to them (that's next). That's the whole memory — and it's already keeping itself balanced. Now go open the shop and put something true in it.

---

## 4. What You Can Ask For

Here's the part where most accounting software hands you a menu of two hundred buttons and a free trial of a CPA. We're not going to do that, because there is no menu. Under the hood your ledger can do exactly **eight things** — and the whole point of this section is that **you never call any of them by name.**

You describe what you want. Your assistant picks the right move. The eight "things" below are really just the eight shapes your intent can take: *record what happened, undo a mistake, confirm something cleared, see where you stand, list a history, pull up one entry, learn the lay of the land,* and *check you're connected.* Notice how few of those are *writing* — there's really only one true write, plus a way to undo it and a way to tick off a confirmation. Everything else is just *reading* your books back to you.

So read this less like a manual and more like a tour of what's possible. You won't be quizzed.

### The Eight Things, in Plain English

#### 1. Record What Happened

This is the one. The heart of the whole ledger and the thing you'll do far more than anything else. You describe a transaction — a sale, a bill, a payment, a paycheck — and the assistant writes it into the books, balanced, with both sides on the right accounts. **Invoices, bills, payments, deposits, payroll, buying a lift — every one of them is this single move.** There is no separate "invoice" feature and no "pay a bill" button; there's just *recording what happened*, into the right accounts.

**You might say:**
> • "Moe paid cash for the $450 transmission job."
> • "Bill Mr. Burns $1,854 for the engine overhaul — he's paying later."
> • "Pay Lenny and Carl their $2,000 wages out of the bank."
> • "Bought the hydraulic lift for $8,000 from the checking account."

You bring the story and the numbers you know; the assistant brings the accounts, the signs, and the side that balances itself.

*(Under the hood: `ledger_record` — one balanced transaction.)*

#### 2. Undo a Mistake — Reverse It

You can't edit or delete an entry (the books are permanent, remember). So when something's wrong, this is the fix: the assistant posts a mirror-image transaction that cancels the bad one, leaving an honest trail. Then you record the right version. Two clean entries instead of a quiet scribble.

**You might say:**
> • "That utilities bill was wrong — back it out."
> • "Scrap that last entry, I'll redo it."
> • "Reverse the $1,500 utilities charge, it should've been $150."

*(Under the hood: `ledger_reverse` — posts the linked, sign-flipped mirror of an entry.)*

#### 3. Confirm Something Cleared

When a payment shows up on your bank or card statement, you mark it confirmed — nudging it from `pending` up to `cleared`, and eventually to `reconciled` once the whole statement ties out. This is the *only* change you can make to an existing entry, and even then it touches nothing but the status — never a dollar amount, never an account.

**You might say:**
> • "These all cleared on the bank statement — mark them cleared."
> • "Everything ties out — lock these in as reconciled."
> • "Burns's deposit showed up on the statement."

*(Under the hood: `ledger_reconcile` — moves one or more postings between `pending` / `cleared` / `reconciled`.)*

#### 4. See Where You Stand — the Balances

This is the workhorse of *reading* your books, and it answers a startling range of questions, all the same way: it totals up accounts and tells you the balance. **What you'd call a "balance sheet," a "profit-and-loss," a "net worth," or a "who owes me" report are all just this one move, pointed at different accounts.** Ask for everything and you see your whole chart of accounts; ask about one bucket or one customer and you get just that slice; ask about a specific month and it totals only that month.

**You might say:**
> • "How much is in the bank right now?"
> • "Who owes me money?" *(totals what's owed to you)*
> • "What did we make and spend in June?" *(a June P&L)*
> • "Where does the shop stand — what do we own, owe, and what's it worth?" *(a balance sheet)*

A "report" here isn't a feature you turn on — it's just this read, aimed at the right accounts for the right stretch of time.

*(Under the hood: `ledger_balance` — totals accounts, optionally filtered by account, by period, or by confirmation status.)*

#### 5. List a History — the Register

Where balances give you *totals*, this gives you the *story*: every entry touching an account, in date order, with a running balance ticking along beside it. This is how you get a **customer statement** ("show me everything on Mr. Burns's account"), an account history, or a plain "list out the transactions." Same idea as a bank statement — a chronological run of activity.

**You might say:**
> • "Show me Mr. Burns's account — everything he's been charged and paid."
> • "List all the transactions that hit the checking account in June."
> • "Walk me through the activity on the credit card."

*(Under the hood: `ledger_register` — the matched entries in date order, with a running total.)*

#### 6. Pull Up One Entry

Sometimes you just want to look at a single transaction in full — every side of it, its confirmation status, and whether it's been reversed. Handy right before you fix something: you pull it up, look at it, *then* reverse it.

**You might say:**
> • "Pull up that wrong utilities entry so I can see it."
> • "Show me the full details of Burns's invoice."

*(Under the hood: `ledger_get` — fetches one transaction in full.)*

#### 7. Learn the Lay of the Land

The orientation move. It reports back the five buckets and what each is for, the confirmation states and what they mean, the live list of accounts you've built up, and the recipes for assembling reports. You'll rarely ask for this yourself — it's mostly how the assistant gets its bearings the first time it opens your books — but it's there, and it's what lets the assistant speak your books fluently from the very first request.

**You might say:**
> • "What kinds of accounts do my books have?"
> • "Get your bearings on my ledger."

*(Under the hood: `ledger_describe` — the discovery call; the first move any assistant makes.)*

#### 8. Confirm You're Connected

The quick "are we actually wired up?" check. The assistant reports back **who you're connected as** — your email — which is handy the very first time you use the ledger, or any time you're not sure the line is live. It changes nothing.

**You might say:**
> • "Am I connected to my books?"
> • "Who am I logged in as?"

*(Under the hood: `ledger_whoami` — reports the connected identity.)*

### Invoices, Bills, and Reports Are Not Separate Features

This is worth saying plainly, because every other accounting tool you've touched has buttons for these and it'll feel like something's missing. It isn't. Watch how the everyday words you already know map onto the moves above:

- An **invoice** (you bill a customer) is just **recording a transaction** that says *this customer now owes me* — money lands in a "what they owe me" account. There is no invoice object; there's a recorded transaction.
- A **bill** (a vendor bills you) is the same move in reverse — **recording a transaction** that says *I now owe this vendor*.
- **Paying** an invoice or a bill is, again, just **recording a transaction** — the money moves and the "owed" account empties out.
- A **report** — balance sheet, P&L, who-owes-me, a customer statement — is never a write at all. It's a **balances** read or a **register** read, pointed at the right accounts.

So the eight moves really are the whole toolbox. Anything that sounds like a fancy accounting feature is one of these moves wearing a costume.

### The Agent Chains These for You

Here's the move that makes the whole thing feel effortless: **a single sentence from you is often several of these steps stitched together.**

When you say *"that utilities bill was wrong — it should've been $150,"* the assistant first **pulls up** the bad entry, then **reverses** it, then **records** the correct one. You said one thing. It did three. You never saw the seams.

This is also why **you never type an ID.** Every transaction has a hidden, computer-generated identifier (a long opaque code, if you're curious), and the assistant keeps track of which one is which. You refer to things the way you'd say them out loud — *"that wrong utilities charge,"* *"Burns's invoice,"* *"the rent check that hasn't cleared"* — and the assistant quietly maps your words to the right entry before it acts.

### The Whole Toolkit, at a Glance

You'll never need this table to *use* your books — you just talk. But if you like seeing the gears, here's how a natural request lines up with what happens behind the scenes:

| What you want | Something you might say | Behind the scenes |
|---|---|---|
| Record what happened (a sale, bill, payment, invoice, payroll) | "Moe paid cash for the $450 job." · "Bill Burns $1,854." | `ledger_record` |
| Undo a mistake | "That entry was wrong — back it out." | `ledger_reverse` |
| Confirm something cleared / lock it in | "These cleared on the statement." · "Reconcile them." | `ledger_reconcile` |
| See where you stand (balances, P&L, who-owes-me, balance sheet) | "How much is in the bank?" · "Who owes me?" · "What did June make?" | `ledger_balance` |
| List a history (a customer statement, an account's activity) | "Show me Burns's account." · "List June's bank activity." | `ledger_register` |
| Pull up one transaction in full | "Show me that utilities entry." | `ledger_get` |
| Get the lay of the land | "What kinds of accounts do I have?" | `ledger_describe` |
| Check you're connected | "Am I connected? Who am I?" | `ledger_whoami` |

Eight moves. Five buckets (you met them in the last section). One thing you ever truly write — a balanced transaction — and a whole lot of ways to read it back. That's the entire system, and from here on the rest of this guide just watches it work, one Springfield repair at a time.

---

## 5. Day One — Opening the Shop

Welcome to *Greased Lightning Auto*. You're Homer Simpson, and you've just signed a lease on a little auto-repair shop in Springfield. You can rebuild a transmission blindfolded, but the *money* side — the books, the ledgers, the "where did the eight thousand dollars go" — that's never been your strong suit. Mr. Plow taught you that. The grease-reselling scheme taught you that twice.

Here's the good news, and it's the whole reason this guide exists: **you don't have to learn bookkeeping. You just have to say what happened.** You tell your assistant "I put ten grand of my own money into the shop today," and it does the precise, double-entry, signs-and-cents part — the part accountants charge by the hour for. You bring the story. The agent brings the arithmetic.

So let's open the shop. Properly. From nothing.

#### First, Is This Thing On?

Before you trust it with a single dollar, make sure the line is live.

**You say:**
> "Are you connected to my ledger? Who does it think I am?"

**What happens:** Your assistant runs the one move whose entire job is to report back who you're signed in as — your owner email and which app is talking to the books. Nothing is created, nothing changes. It's the handshake that proves the whole chain — your assistant, the connection, your sign-in, the service — is wired up before you start trusting it. *(Under the hood: `ledger_whoami()` — takes no input, just confirms the connection.)*

**You get back:**
> "You're connected as **homer@greasedlightning.example**. The books are open and ready."

**Say it your way:**
> • "Quick check — is the ledger online?"
> • "Who am I logged in as?"
> • "Make sure you can reach my books."

If that comes back clean, you're in business. If it doesn't, that's your cue to fix the connection — not your data.

#### Meet the Five Buckets

You've never kept a set of books before, so before you post a single thing, ask the ledger to introduce itself.

**You say:**
> "I've never done this before. What can these books actually hold? Walk me through it."

**What happens:** Your assistant calls the ledger's "tell me about yourself" move and translates the answer into plain English. Every dollar that ever moves through your shop lands in one of exactly **five buckets** — and that's the entire chart of accounts you'll ever need to understand:

- **Assets** — *stuff you own.* Cash in the till, money in the bank, the hydraulic lift, money customers owe you.
- **Liabilities** — *money you owe.* The startup loan, the credit-card balance, the sales tax you've collected but haven't sent to the state yet.
- **Equity** — *your stake.* What you put in, plus the profit the shop has piled up over time.
- **Income** — *money you earn.* Labor, parts you sell, the work that pays the bills. (The books also answer to **Revenue** — same bucket, two names.)
- **Expenses** — *money you spend.* Wages, rent, the power bill, the parts you buy.

That's it. Five buckets. You'll never create a sixth, and you'll never have to "set up" an account before using it — more on that in a second. *(Under the hood: `ledger_describe()` — the first call any agent makes. It returns the five typed roots, the money unit (`USD cents`), the reconciliation states, the live account tree, and a set of recipes for building reports. Takes no input.)*

> **The one fact worth keeping.** Everything you own, owe, earn, or spend lands in one of five buckets: **Assets, Liabilities, Equity, Income, Expenses.** You never type those words. You describe what happened, and the agent files it under the right one.

#### T1 — Homer Puts In $10,000

Time to fund the shop. You're moving ten thousand dollars of your own personal savings into the business checking account at Springfield Savings.

**You say:**
> "I'm putting ten thousand dollars of my own money into the shop. Opening capital. Today."

**What happens:** Here's where you meet the one idea that makes double-entry tick — and the assistant is about to handle all of it for you. Every entry has **two sides that balance.** Money showed up in your bank account (one side), and it came from *you, the owner* (the other side). So the agent records the $10,000 landing in the bank, and the matching $10,000 as your stake in the business — your **Equity**.

And here's the ergonomic trick you'll lean on forever: **you only said one number — ten thousand — and the agent figured out the other side itself.** You name the amount you actually know; the balancing side takes whatever makes the two sides cancel to zero. Accountants call this "the entry balances." You can call it "I said the number, it did the rest."

*(Under the hood: `ledger_record("2026-06-01", "Owner's opening capital", [{Assets:Bank:Checking, +1000000}, {Equity:OwnerCapital (elided → −1000000)}])`. You said `$10,000`; the agent stored `1000000` cents. The `Equity:OwnerCapital` leg has no amount written down — it's **elided**, so it soaks up the exact balancing residual, here `−1000000`. The two signed amounts sum to zero, which is the law every transaction obeys.)*

**You get back:**
> "Recorded. $10,000 of owner's capital — it's in your checking account, and it's logged as your stake in the shop."

**Say it your way:**
> • "Fund the shop with $10,000 of my savings."
> • "Log my $10,000 startup investment into checking."
> • "I'm seeding the business with ten grand of my own cash."

Notice what you didn't do: you didn't say "debit," you didn't say "credit," you didn't pick an account name out of a list, and you only typed *one* dollar figure. That's the whole deal, and it doesn't change no matter how big the shop gets.

#### T2 — The Startup Loan, $15,000

Ten grand of your own money is a start, but a lift and two mechanics cost more than that. Springfield Savings is lending you fifteen thousand.

**You say:**
> "Springfield Savings just gave me a $15,000 startup loan. It's in the checking account."

**What happens:** Same two-sided shape, different second bucket. The $15,000 lands in your bank (an **Asset** going up), and the matching side records that you now *owe* the bank fifteen grand (a **Liability**). Borrowed money isn't income — it's not yours, you have to pay it back — so it never touches the Income bucket. The agent knows that distinction so you don't have to. *(Under the hood: `ledger_record("2026-06-01", "Startup loan — Springfield Savings", [{Assets:Bank:Checking, +1500000}, {Liabilities:Loan:SpringfieldSavings (elided → −1500000)}])`. Again you named one number, $15,000; the loan leg is elided and absorbs the `−1500000` balancing residual.)*

**Say it your way:**
> • "Record a $15,000 loan from the bank into checking."
> • "I borrowed fifteen grand from Springfield Savings to get started."
> • "Log the startup loan — $15k, Springfield Savings."

> **Capital vs. loan — the agent already knows the difference.** Money *you* put in is **Equity** (your stake). Money you *borrow* is a **Liability** (a debt). Both put cash in the bank; they part ways on the other side. You just say "my money" or "a loan," and the right bucket gets picked.

#### T3 — Buy the Hydraulic Lift, $8,000

You can't fix a car on the floor. You buy a hydraulic lift for eight thousand dollars, straight out of the checking account.

**You say:**
> "I bought a hydraulic lift for $8,000, paid from checking."

**What happens:** This one's interesting — it's the first entry where *both* sides are things you own. Cash leaves the bank (an Asset going **down**), and in its place you've got a lift (a different Asset going **up**). Your *total* wealth didn't change; you just swapped eight grand of cash for eight grand of equipment. No income, no expense — the lift is a thing the shop owns, not money spent and gone. *(Under the hood: `ledger_record("2026-06-01", "Hydraulic lift", [{Assets:Equipment:Lift, +800000}, {Assets:Bank:Checking, −800000}])`. Two real numbers this time — nothing elided — because you're telling it exactly where $8,000 went and exactly where it came from. They sum to zero, as always.)*

**Say it your way:**
> • "Spent $8,000 from the bank on a hydraulic lift."
> • "Log the lift purchase — eight grand, out of checking."
> • "We bought equipment: a lift, $8,000, paid by bank."

A small thing to notice: you never created an account called `Assets:Equipment:Lift`. You just mentioned a lift, and the account **sprang into existence** the moment you posted to it. That's true of every account in this whole system.

> **Accounts are born when you first use them — there's no setup step.** You never "create an account." The first time you mention the till, the credit card, or Mr. Burns's tab, the right account appears on its own. The agent only has to keep the *bucket* right: every account hangs off one of the five roots. The shape below the root — `Bank:Checking`, `Equipment:Lift` — is just the agent's tidy filing convention.

#### Watch the Chart of Accounts Appear From Nothing

You've made three entries. Let's see what they built.

**You say:**
> "Show me where everything stands right now."

**What happens:** Your assistant pulls the full picture — every account that now exists, with its balance — and reads it back to you in plain dollars. A minute ago this was empty. Now there's a whole little chart of accounts, and *you never designed it* — it grew out of the three things you told the books. *(Under the hood: `ledger_balance()` with no arguments at all — the whole emergent account tree with raw balances. This single tool is the trial balance, the balance sheet, net worth, and per-customer tabs, all depending on how you ask.)*

**You get back:**
> "Here's the shop so far:
> - **In the bank:** $17,000
> - **The hydraulic lift:** $8,000
> - **Owner's capital (your stake):** $10,000
> - **Loan owed to Springfield Savings:** $15,000
>
> You own **$25,000** in stuff, you owe **$15,000**, and your stake is **$10,000**. It all ties out."

(The bank shows $17,000 because $10,000 in plus $15,000 in minus $8,000 for the lift leaves $17,000.)

> **The books always balance to exactly zero — that's a free correctness check.** Add up *every* account with its raw sign and you get zero, every single time. It's not magic; it's the consequence of every entry having two sides that cancel. If it ever *didn't* sum to zero, something would be wrong — and it never will, because the agent won't let an unbalanced entry post.

**Where we stand.** In four plain sentences — a handshake, a "teach me," and three "here's what happened" — you opened a shop from nothing. There's money in the bank, a lift on the floor, your stake on the books, and a loan to pay back. You never typed an account name, never said "debit" or "credit," and only ever named the numbers you actually knew. Next, the doors open and the cars roll in.

---

## 6. The Daily Rhythm — Sales, Invoices & Bills

This is the part of owning a shop nobody romanticizes: the steady drip of jobs done, parts bought, people paid. It's also where you'll do ninety percent of your bookkeeping — and you'll do all of it the same way, by describing what happened. Sales, invoices, bills, a credit-card swipe, payroll, rent. Watch how the *same* handful of moves handles every one.

#### T4 — Moe Pays Cash for a Transmission, $450

First customer: Moe Szyslak rolls in with a slipping transmission. You rebuild it, he peels four hundred and fifty bucks off a roll and drops it in the till. The simplest sale there is.

**You say:**
> "Moe paid cash for the $450 transmission job."

**What happens:** Cash came in (an **Asset** — the till — goes up), and you *earned* it doing labor (that's **Income**). The agent puts $450 in the drawer and books $450 of labor revenue, balanced. One number from you; the income side balances itself. *(Under the hood: `ledger_record("2026-06-04", "Moe — transmission, cash", [{Assets:Cash:Drawer, +45000}, {Income:Service:Labor (elided → −45000)}])`. $450 is `45000` cents; the labor leg is elided and takes the `−45000` residual. Income is stored negative under the hood; the agent always flips it right-side-up, so you only ever hear "$450 in revenue." More on that sign convention in the FAQ at the back.)*

**You get back:**
> "Got it — $450 in the till from Moe, booked as labor revenue."

**Say it your way:**
> • "Cash sale — Moe, $450 for a transmission rebuild."
> • "Took in $450 cash from Moe for the transmission."
> • "Moe's transmission, four-fifty, paid in cash to the drawer."

#### T5 — Deposit the Till at the Bank, $450

That cash shouldn't sleep in the drawer. End of day, you walk it to Springfield Savings.

**You say:**
> "I deposited Moe's $450 from the till into the bank."

**What happens:** Just like the lift, this is an asset-to-asset shuffle — money leaves one pocket (the till) and lands in another (the bank). Your total cash didn't change; it just moved somewhere safer. No income here — Moe's payment was already booked as revenue back when he handed it over; depositing it again would double-count. The agent knows the difference. *(Under the hood: `ledger_record("2026-06-04", "Deposit cash to bank", [{Assets:Bank:Checking, +45000}, {Assets:Cash:Drawer, −45000}])`. The till goes back to $0; the bank rises by $450.)*

**Say it your way:**
> • "Move the $450 from the drawer to checking."
> • "Banked the day's cash — $450."
> • "Deposited the till into Springfield Savings."

#### T6 — Invoice Ned Flanders, $309 on Account

Ned Flanders — neighborino, pays on time, diddly-doo — needs a brake job. But Ned doesn't have cash on him; he'll pay you next week. So you do the work and send him a bill. In bookkeeping, that bill is called an **invoice**, and here's the thing to internalize:

> **An invoice isn't a special feature — it's just a sale where the money is "owed to you" instead of "in hand."** When a customer will pay later, the money you're owed is itself an Asset, called a **receivable** (A/R). You record the sale exactly like a cash sale, except the cash side becomes "Ned owes me."

The brake job breaks down into **$200 of labor**, **$100 of parts**, and **$9 of sales tax** you have to collect on the parts. That's three things you earned-or-owe, balanced against the one $309 total Ned owes you.

**You say:**
> "Invoice Ned Flanders for the brake job — $200 labor, $100 in parts, plus $9 sales tax. He'll pay later."

**What happens:** The agent opens a tab for Ned — a receivable — for the full $309. Against it: $200 of labor income, $100 of parts income, and $9 of sales tax. That last one is sneaky-important: the sales tax was never *yours*. You collected it from Ned on the state's behalf, and you owe it to the state — so it goes into a **Liability**, not income. The agent splits the three pieces and balances them against Ned's tab automatically. *(Under the hood: `ledger_record("2026-06-07", "Ned Flanders — brake job (invoice)", [{Assets:Receivable:Flanders, +30900}, {Income:Service:Labor, −20000}, {Income:Service:Parts, −10000}, {Liabilities:SalesTaxPayable, −900}])`. Four legs: Ned owes `30900`; labor `−20000`, parts `−10000`, sales tax `−900`. They sum to zero.)*

**You get back:**
> "Invoiced Ned $309 — $200 labor, $100 parts, $9 sales tax. It's on his tab until he pays."

**Say it your way:**
> • "Bill Ned $309 for brakes, on account — labor $200, parts $100, tax $9."
> • "Ned's brake job goes on his tab: $309 total, with the parts/labor/tax split."
> • "Put Ned down for $309 owed — brake work, he pays later."

Notice you described the *shape* of the job — labor, parts, tax — and the agent mapped each piece to the right bucket. You never said "receivable" or "liability." You said "he'll pay later" and "sales tax," and the agent knew what those mean.

#### T7 — Ned Pays, $309

A week later, true to form, Ned mails a check.

**You say:**
> "Ned paid his $309 invoice."

**What happens:** Ned's tab closes. Money lands in the bank (Asset up), and his receivable — the "Ned owes me" — drops to zero (that Asset goes down by the same amount). Note what *doesn't* happen: no income is recorded here. You already booked the labor, parts, and tax back when you did the work; the payment is just the receivable turning into cash. Double-counting averted, automatically. *(Under the hood: `ledger_record("2026-06-12", "Ned Flanders — payment", [{Assets:Bank:Checking, +30900}, {Assets:Receivable:Flanders (elided → −30900)}])`. You named $309; the receivable leg is elided and takes the `−30900` that zeroes Ned out.)*

**Say it your way:**
> • "Ned's check came in — $309, mark him paid."
> • "Close out Ned's tab, he paid in full."
> • "Record Ned's $309 payment to the bank."

> **A receivable's whole life: it opens when you invoice, and it closes when they pay.** Ned's was born on June 7 and died on June 12 — quick and clean, the Flanders way. The *next* customer won't be so prompt.

#### T8 — Invoice Mr. Burns, $1,854 on Account

Mr. Burns — owner of the nuclear plant, richer than God, and *constitutionally incapable of paying a bill on time*. His Bentley needs an engine overhaul. You do the work: **$1,200 labor**, **$600 parts**, **$54 sales tax** — $1,854 all in, on account. This is the invoice we're going to follow all the way to the end of the guide, because Burns is going to make us wait.

**You say:**
> "Invoice Mr. Burns for the engine overhaul — $1,200 labor, $600 parts, $54 sales tax, on account."

**What happens:** Exactly the same shape as Ned's invoice, just bigger numbers and a different customer's tab. Burns now owes the shop $1,854; against it sit $1,200 labor income, $600 parts income, and $54 collected sales tax (that Liability again). *(Under the hood: `ledger_record("2026-06-18", "Mr. Burns — engine overhaul (invoice)", [{Assets:Receivable:Burns, +185400}, {Income:Service:Labor, −120000}, {Income:Service:Parts, −60000}, {Liabilities:SalesTaxPayable, −5400}])`. Burns's tab: `185400`. Labor `−120000`, parts `−60000`, tax `−5400`. Sums to zero.)*

**You get back:**
> "Invoiced Mr. Burns $1,854 — $1,200 labor, $600 parts, $54 tax. It's on his tab."

**Say it your way:**
> • "Bill Burns $1,854 for the engine job, on account."
> • "Put Mr. Burns down for an $1,854 overhaul — labor, parts, and tax — he'll pay later."
> • "Mr. Burns's overhaul goes on his tab: $1,200 labor, $600 parts, $54 tax."

Mr. Burns's tab is now **open** — and unlike Ned's, it's going to *stay* open. Remember it. We'll check on it at month-end, and it'll still be sitting there, gathering dust and interest in Mr. Burns's heart.

#### T9 — Parts on Account From Crazy Vaclav, $600

You needed parts for Burns's overhaul, and you got them from Crazy Vaclav, purveyor of automobiles and components ("it has good times in it!"). Vaclav doesn't make you pay on the spot — he sends a bill. A vendor bill is the mirror image of an invoice: instead of someone owing *you*, *you* owe someone.

**You say:**
> "Crazy Vaclav sent a $600 bill for parts. I'll pay him later."

**What happens:** The parts are an **Expense** the moment you buy them (this ledger has no inventory — parts you buy are expensed right away, which is correct and intended). And because you haven't paid yet, the other side is a **payable** (A/P) — a Liability, the money you owe Vaclav. *(Under the hood: `ledger_record("2026-06-20", "Crazy Vaclav — parts (on account)", [{Expenses:Parts, +60000}, {Liabilities:Payable:Vaclav (elided → −60000)}])`. One number, $600; the payable leg is elided and absorbs `−60000`.)*

**Say it your way:**
> • "Record a $600 parts bill from Vaclav, on account."
> • "I owe Crazy Vaclav $600 for parts — bill me later."
> • "Log Vaclav's parts invoice — six hundred bucks, unpaid."

> **A bill is an invoice in reverse.** An invoice = a customer owes *you* (a receivable, an Asset). A bill = *you* owe a vendor (a payable, a Liability). Same machinery, mirror direction. You just say "they'll pay me later" or "I'll pay them later," and the agent picks the right side.

#### T10 — Brake Pads on the Shop Credit Card, $120

You run low on brake pads and grab a box on the shop's credit card — a hundred and twenty bucks.

**You say:**
> "I bought $120 of brake pads on the shop credit card."

**What happens:** Parts again, so it's an **Expense** the moment you buy them — same as Vaclav's. The difference is *how* you paid: not cash, not "I'll pay the vendor later," but on the card. So the other side is your **credit-card balance**, a Liability that goes up by $120. *(Under the hood: `ledger_record("2026-06-22", "Brake pads — shop card", [{Expenses:Parts, +12000}, {Liabilities:CreditCard (elided → −12000)}])`. $120 = `12000`; the card leg is elided.)*

**Say it your way:**
> • "Put $120 of brake pads on the card."
> • "Charged brake pads to the shop credit card — $120."
> • "Bought pads, $120, credit card."

#### T11 — Pay Vaclav's Bill, $600

Time to settle up with Vaclav before he gets *crazy*.

**You say:**
> "I paid Crazy Vaclav's $600 bill from the bank."

**What happens:** The payable you opened back in T9 now closes. You owe Vaclav less (the Liability goes down by $600), and the bank goes down by $600. No new expense — you already expensed the parts when you got them; this is just settling the debt. *(Under the hood: `ledger_record("2026-06-25", "Crazy Vaclav — pay bill", [{Liabilities:Payable:Vaclav, +60000}, {Assets:Bank:Checking, −60000}])`. The payable goes to zero; the bank drops $600.)*

**Say it your way:**
> • "Pay off Vaclav — $600 from checking."
> • "Settle the Vaclav parts bill, six hundred, from the bank."
> • "Clear what I owe Crazy Vaclav."

#### T12 — Wages for Lenny & Carl, $2,000

You've got two mechanics — Lenny and Carl — and payday's here. Two grand, out of the bank.

**You say:**
> "Paid Lenny and Carl their wages — $2,000 total, from the bank."

**What happens:** Wages are an **Expense** (money spent and gone — unlike the lift, you don't get a thing back you can resell). The bank drops $2,000 to cover it. *(Under the hood: `ledger_record("2026-06-27", "Wages — Lenny & Carl", [{Expenses:Wages, +200000}, {Assets:Bank:Checking, −200000}])`.)*

**Say it your way:**
> • "Payroll — $2,000 to the mechanics, from checking."
> • "Pay the guys: Lenny and Carl, two grand."
> • "Log $2,000 in wages, paid by bank."

#### T13 — Rent for June, $900

Last regular bill of the month: rent. You write the landlord a check for $900.

**You say:**
> "Paid June rent — $900, by check from the bank."

**What happens:** Rent's an **Expense**; the bank covers it. Routine. *(Under the hood: `ledger_record("2026-06-28", "Shop rent — June", [{Expenses:Rent, +90000}, {Assets:Bank:Checking, −90000}])`.)*

**Say it your way:**
> • "Log June's rent — $900 from checking."
> • "Rent check's out — nine hundred bucks."
> • "Record the $900 shop rent for June."

Keep an eye on this rent check. You *wrote* it on June 28, but the landlord — like most landlords — won't get around to depositing it for a couple of weeks. That gap is going to matter when we reconcile against the bank statement. Hold that thought.

#### Oops — Honest Books and the Reversal

Here's the most important thing in this whole chapter, and it starts, as these things do, with a screw-up.

You go to record the June power bill. The utilities ran **$150**. But it's late, you're tired, and you fat-finger it as **$1,500**.

**You say:**
> "Log June utilities — $1,500, from the bank."

**What happens:** The agent dutifully records exactly what you said: $1,500 of utilities expense, $1,500 out of the bank. Garbage in, garbage faithfully recorded. *(Under the hood: `ledger_record("2026-06-30", "Utilities — June", [{Expenses:Utilities, +150000}, {Assets:Bank:Checking, −150000}])`. That's `150000` cents — fifteen hundred dollars. Wrong by a factor of ten.)*

A minute later you realize the power company isn't Mr. Burns; nobody's bill is $1,500. So you ask to look at what you just entered.

**You say:**
> "Pull up that utilities entry I just made — let me see it."

**What happens:** The agent fetches the full transaction so you can eyeball it: both legs, the amounts, the date. Yep — there it is, $1,500, plain as day, wrong. *(Under the hood: `ledger_get(<the wrong entry's id>)` returns the whole transaction — every posting, its status, and any reversal links. You never type that id — the assistant tracks which entry "that utilities one" means.)*

Now — and this is the part that surprises people — **you cannot edit it.** There's no "change $1,500 to $150" button. There's no edit at all, and no delete. The journal is permanent, append-only, set in ink. That sounds maddening until you realize *why*: a set of books anyone could quietly reach into and rewrite is a set of books nobody can trust. Yours stays honest precisely because the past can't be silently altered.

So how do you fix a mistake? You **reverse it** — you post a perfect mirror image that cancels the bad entry out — and then you record it correctly. The mistake, the cancellation, and the correction all stay in the book, a complete and honest trail of what really happened.

**You say:**
> "That's wrong — it should be $150, not $1,500. Reverse it and fix it."

**What happens, step one (the reversal):** The agent posts the sign-flipped mirror of the bad entry — $1,500 *back* into the bank, $1,500 of utilities expense *removed*. Now the original and its mirror net to exactly zero; it's as if the mistake never hit your balances, except it's all still there on the record. *(Under the hood: `ledger_reverse(<that entry's id>)` → a linked mirror `[{Expenses:Utilities, −150000}, {Assets:Bank:Checking, +150000}]`. The two transactions are cross-linked (`reverses_id` / `reversed_by_id`), and the mirror's legs come back marked `pending`.)*

**What happens, step two (the correction):** The agent records the utilities bill the way it should have been — $150.

*(Under the hood: `ledger_record("2026-06-30", "Utilities — June (corrected)", [{Expenses:Utilities, +15000}, {Assets:Bank:Checking, −15000}])`. That's `15000` cents — one hundred fifty dollars. Correct.)*

**You get back:**
> "Done. I reversed the $1,500 entry and re-recorded utilities at $150. All three are on the books — the original, the reversal, and the fix — so the trail's honest. Your real utilities expense for June is $150."

**Say it your way:**
> • "Scrap that $1,500 utilities entry and redo it at $150."
> • "I keyed utilities wrong — back it out and post the right $150."
> • "Undo the bad power bill and enter the correct one."

> **You never erase — you reverse.** A mistake gets fixed by posting its mirror (which cancels it) and then recording the correct version. The wrong entry stays visible forever, neutralized but not hidden. This is the ledger's honesty guarantee, and it's the same instinct as a real accountant's: you don't rub out ink, you post a correcting entry. (One note: the bad entry and its reversal cancel to zero and represent *no real bank movement* — the money never actually left — so they stay `pending` and won't muddy the bank reconciliation we're about to do.)

**Where we stand.** Two weeks of business, all of it captured by just *talking*: a cash sale and its deposit, two invoices (one paid, one stubbornly open), a vendor bill paid off, a card swipe, payroll, rent, and a fat-fingered utility bill caught and corrected the honest way. You've now used the record-it, fetch-it, and reverse-it moves — and every single entry balanced to zero without you ever once saying "debit." Next, the month closes: we reconcile against the bank and read the shop's first real reports.

---

## 7. End of Month — Reconcile & Report

June's over. Now comes the part that separates a shoebox of receipts from an actual set of books: you check your records against reality (the bank), and then you ask the books what they've learned. Both are things you do by *asking* — there's no "close the month" button, just the same handful of moves pointed at month-end questions.

### Reconciling the Bank

Your books say one thing about your bank balance. Springfield Savings' statement says another. The job of a **bank reconciliation** is to make those two agree — and to understand any difference. Every posting carries a little status that tracks exactly this:

- **pending** — recorded, but you haven't confirmed it against the outside world yet (the default).
- **cleared** — you've seen it on the bank statement; it really happened.
- **reconciled** — matched against the official statement balance and locked in.

Walking a posting from `pending` to `cleared` to `reconciled` is the whole reconciliation lifecycle. Let's do it.

#### Step One — List the Bank's History

**You say:**
> "Show me everything that's hit the checking account this month."

**What happens:** The agent pulls every posting that touched the bank, in date order, with a running balance — so you can sit there with the paper statement and tick down the list line by line. This is also where the agent quietly grabs the internal handle for each line (its `posting_id`), which it'll need in a moment to mark things cleared. *(Under the hood: `ledger_register(query:"Assets:Bank")` → matched postings in chronological order with a running total, each carrying its `posting_id`. The substring `Assets:Bank` matches every bank sub-account — here that's just `Assets:Bank:Checking`.)*

You go down the list against the statement. Everything matches the bank — the capital, the loan, the lift, Moe's deposit, Ned's payment, the Vaclav payment, wages, the corrected utilities — **except one thing**: the **$900 rent check (T13)**. You wrote it June 28, but the landlord hasn't cashed it yet, so it isn't on the bank's statement. (Remember, we flagged this.) The fat-fingered utilities entry and its reversal also show up here, but they net to zero and were never real money, so they stay `pending` and sit out the reconciliation entirely.

#### Step Two — Mark the Cleared Items

**You say:**
> "Everything cleared the bank except the rent check. Mark the rest as cleared."

**What happens:** The agent flips the status of every *real* bank movement that showed up on the statement from `pending` to `cleared` — all of them except the rent check (and the wrong-utilities entry and its reversal stay `pending`, as we noted; they were never real money). This is the *one and only* thing in the whole ledger that's allowed to change an existing row, and even then it only touches the *status*: never an amount, never an account, never a date. *(Under the hood: `ledger_reconcile([<cleared bank posting_ids>], "cleared")` — the only permitted mutation of existing journal rows. It's all-or-nothing: if even one posting_id is bad, the whole call fails, so you never end up half-done.)*

#### Step Three — Read the Difference

Now the satisfying part: ask the books to show you the gap between "what the bank has cleared" and "what my ledger says," and confirm it's exactly the one thing you expect.

**You say:**
> "What's my checking balance on the books versus what's actually cleared the bank?"

**What happens:** The agent runs the bank balance two ways — once counting everything, once counting only the cleared items — and hands you both:

- **Ledger balance** (everything): **$14,109** *(`ledger_balance(query:"Assets:Bank")`)*
- **Cleared balance** (only what's hit the bank): **$15,009** *(`ledger_balance(query:"Assets:Bank", status:"cleared")`)*
- **The $900 difference between the two** — exactly the outstanding rent check.

The cleared balance is *higher* because the rent hasn't actually left the bank yet — the bank still thinks you have that $900. The moment the landlord cashes the check (next month), the two will agree. **You've explained the entire difference with a single outstanding item**, which is precisely what a clean reconciliation looks like.

**You get back:**
> "Your books show **$14,109** in checking. The bank's cleared total is **$15,009**. The **$900** gap is the June rent check — written, but the landlord hasn't cashed it yet. Everything else ties out perfectly."

#### Step Four — Lock It In

The numbers tie out, so you make it official.

**You say:**
> "Great, it reconciles. Lock in the cleared items as reconciled."

**What happens:** The agent walks those same cleared postings one more rung — from `cleared` to `reconciled` — the locked-in, "matched-against-the-official-statement" state. The rent check stays **pending**; it hasn't cleared, so there's nothing to reconcile yet. (It'll get its turn in July.) *(Under the hood: `ledger_reconcile([...], "reconciled")`. Transitions are free in any direction, so the agent can move postings forward — or back, if you ever need to.)*

**Say it your way (for the whole reconciliation):**
> • "Reconcile the checking account against the June statement — everything but the rent cleared."
> • "Match my bank entries to the statement; the rent check's still outstanding."
> • "Tie out the bank for June and lock the cleared stuff in."

> **The posting lifecycle, walked end to end:** every bank entry started **pending**, became **cleared** when you saw it on the statement, and got locked as **reconciled** when the statement balance matched. The one exception — the rent check — sits at **pending** until it clears in July. That's the whole reconciliation discipline, and you drove it entirely by describing what cleared.

### The Reports — Asking the Books What They Know

Here's a thing worth saying out loud: **there is no "reports" feature.** No "run the P&L" button, no balance-sheet template. Every report in this guide is just a *question* you ask the books — the same balance-and-history moves you've already used, pointed at the right slice. The agent knows the recipes. You just ask.

#### Who Owes Me Money?

**You say:**
> "Who still owes me money?"

**What happens:** The agent totals up every customer tab — your accounts receivable — and reads back who's outstanding. Ned's tab is closed (he paid), so he's at zero. Mr. Burns, of course, is still sitting there. *(Under the hood: `ledger_balance(query:"Assets:Receivable")` — and because each customer has their own sub-account, the per-customer breakdown falls out for free.)*

**You get back:**
> "One open tab: **Mr. Burns owes $1,854.** Ned Flanders is paid up. That's your total receivables — **$1,854.**"

#### Mr. Burns's Statement

You want to send Burns a reminder, so you ask for his statement — every charge and payment on his tab, in order.

**You say:**
> "Show me Mr. Burns's account history."

**What happens:** The agent pulls the chronological history of Burns's tab, with a running balance — which is exactly what a customer statement *is*. Right now it's a short story: one charge, no payments. *(Under the hood: `ledger_register(query:"Assets:Receivable:Burns")` — the chronological postings with a running total. This recipe is the customer statement.)*

**You get back:**
> "**Mr. Burns — statement:**
> - Jun 18 — Engine overhaul (invoice): **$1,854** · running balance **$1,854**
>
> Still open. No payments yet."

#### June's Profit & Loss — the $1,220 Surprise

The big question: did the shop make money in June?

**You say:**
> "Did I make a profit in June? Show me income versus expenses."

**What happens:** The agent runs two questions over the month of June — total income, total expenses — and does the subtraction. Brace yourself. *(Under the hood: `ledger_balance(query:"Income", period:"2026-06")` → revenue **$2,550**; `ledger_balance(query:"Expenses", period:"2026-06")` → expenses **$3,770**. The ledger computes net income in raw signed terms as −(Income + Expenses) = −(−$2,550 + $3,770) = **−$1,220**; presented human-side-up, that's just revenue $2,550 − expenses $3,770 = a **$1,220 loss**. The agent always shows you the human-side-up version.)*

**You get back:**
> "Brace yourself, Homer. June revenue was **$2,550**. June expenses were **$3,770**. That's a **$1,220 loss** for the month.
>
> Don't panic — this is a *normal* first month. You bought a lift, paid two mechanics for a full month, covered rent, and a chunk of your parts spending was for jobs you billed but haven't been paid for yet. **Revenue isn't profit**, and month one almost always runs red while you're still buying the shop."

**Say it your way:**
> • "What's my June P&L?"
> • "Run a profit-and-loss for last month."
> • "How'd the shop do in June — up or down?"

#### The Balance Sheet — and a Small Accounting Mystery

Now the other big report: a snapshot of what the shop owns, owes, and is worth *right now*.

**You say:**
> "Give me a balance sheet — what do I own, what do I owe, and what's my stake?"

**What happens:** The agent runs three questions — total Assets, total Liabilities, total Equity — and lays them out. *(Under the hood: `ledger_balance(query:"Assets")` = **$23,963** (Bank $14,109 + Receivable $1,854 + Equipment $8,000 + Cash $0); `ledger_balance(query:"Liabilities")` = **$15,183** (Loan $15,000 + Card $120 + SalesTax $63); `ledger_balance(query:"Equity")` = **$10,000** (your OwnerCapital).)*

**You get back:**
> "**Balance sheet, right now:**
> - **You own:** $23,963 — $14,109 in the bank, $1,854 owed to you by Burns, the $8,000 lift, nothing in the till.
> - **You owe:** $15,183 — the $15,000 loan, $120 on the card, $63 of sales tax due the state.
> - **Your stake:** $10,000 — the capital you put in."

Now look closely, because there's a puzzle here — and it's the single most illuminating thing in this whole guide.

Your stuff is worth **$23,963**. But your stake plus your debts is $10,000 + $15,183 = **$25,183**. Those don't match — they're off by exactly **$1,220**. That number should look familiar: it's June's loss.

Here's what's going on. That $1,220 loss is *still sitting inside the Income and Expenses accounts* — it hasn't been moved into your Equity yet. Properly, your stake *including* the month's result is $10,000 − $1,220 = **$8,780**. And then it all clicks: Liabilities $15,183 + true Equity $8,780 = **$23,963** = your Assets. The books were never wrong — the loss just hadn't been folded into your stake.

> **This is exactly why bookkeepers "close the books."** The shop's earning accounts (Income and Expenses) hold the running result until you formally sweep it into Equity. Until then, the balance sheet's equity line shows your *contributed* stake, not your stake-plus-or-minus-profit. The raw ledger keeps everything summing to zero by leaving Income and Expenses open all year — and that gap of "unclosed profit" is the whole reason the End-of-Year chapter exists. Hold onto this; it pays off in Section 8.

#### Net Worth in One Line

For a quick gut-check, you can ask for the five buckets rolled up to their top level.

**You say:**
> "Just give me the big picture — the five buckets, top level."

**What happens:** The agent rolls every account up to its root and reads back the five totals, from which net worth (Assets + Liabilities + Equity) falls right out. *(Under the hood: `ledger_balance(depth:1)` — roll everything up to the root level, one line per bucket. This is the all-roots roll-up the net-worth recipe builds on.)*

### T17 — Burns Finally Pays (July)

Fast-forward into July. The June rent check finally clears the bank. And — wonder of wonders — **Mr. Burns pays his bill.** Only took him three weeks past the work and a few pointed reminders, but the check is good.

**You say:**
> "Mr. Burns finally paid his $1,854 invoice."

**What happens:** The receivable that's been sitting open since June 18 closes at last. Money lands in the bank; Burns's tab drops to zero. No income recorded — you booked the labor, parts, and tax back in June when you did the work; this is just the tab turning into cash. The cradle-to-grave arc of one invoice, completed across a month boundary. *(Under the hood: `ledger_record("2026-07-09", "Mr. Burns — payment", [{Assets:Bank:Checking, +185400}, {Assets:Receivable:Burns (elided → −185400)}])`. You named $1,854; the receivable leg is elided and takes the `−185400` that zeroes Burns out.)*

**You get back:**
> "Hallelujah. Mr. Burns paid $1,854 — it's in the bank, and his tab is officially closed."

**Say it your way:**
> • "Burns's check came in — $1,854, mark him paid."
> • "Close out Mr. Burns's tab, he finally paid up."
> • "Record Burns's $1,854 payment to checking."

Ask for his statement now and the whole story's there — the June charge, the July payment, running balance **$0**:

> **Mr. Burns — statement:**
> - Jun 18 — Engine overhaul (invoice): $1,854 · running **$1,854**
> - Jul 9 — Payment: −$1,854 · running **$0**

That deposit posts `pending`; when it shows up on July's bank statement you'll mark it `cleared`, then `reconciled` — the same lifecycle you just walked. (And the June rent check, which finally cleared in July? You'll reconcile that on the July statement too, and the $900 gap from June closes for good.)

**Where we stand.** You reconciled June against the bank and explained the one difference to the penny. You pulled a receivables list, a customer statement, a profit-and-loss (a humbling $1,220 loss — and you learned *why* that's fine), and a balance sheet (which handed you the single best lesson in bookkeeping: the unclosed-profit gap). Then you watched Mr. Burns's invoice complete its full life, June to July, born to closed. Every report was just a *question* — no new tool, no new verb. One question remains, and it's the one the balance sheet teed up: what happens at year-end, when you finally close the books?

---

## 8. End of Year — Closing the Books

Let's jump ahead. Way ahead — past July, past the summer rush, past the holiday slowdown, all the way to **December 31, 2026**. Greased Lightning Auto has been open seven months now, and the numbers from here on are the **full-year totals** — not the little June slice we followed step by step, but the whole year added up. (We're skipping the hundreds of entries in between; just know they happened, the same plain way, one "here's what happened" at a time.)

And the news is good. The shop found its feet.

**You say:**
> "Show me the whole year — total income and total expenses for 2026."

**What happens:** The agent runs the same profit-and-loss recipe you used for June, just over the full year instead of one month. *(Under the hood: `ledger_balance(query:"Income", period:"2026")` → revenue **$82,000** (raw `−8,200,000`); `ledger_balance(query:"Expenses", period:"2026")` → expenses **$61,000** (raw `+6,100,000`). Net profit for the year = −(Income + Expenses) = −(−$82,000 + $61,000) = **$21,000**; human-side-up, that's revenue $82,000 − expenses $61,000.)*

**You get back:**
> "Big year, Homer. Full-year revenue: **$82,000**. Full-year expenses: **$61,000**. The shop cleared a **$21,000 profit** for 2026. You did it."

That $21,000 is real, and it's yours. But here's the thing the June balance sheet warned us about: **right now that profit is still trapped inside the Income and Expenses accounts.** Your *stake* on the balance sheet still reads $10,000 — your original capital — as if the shop had earned nothing all year. To make the books tell the truth about your stake, you have to **close the year.**

#### Temporary vs. Permanent — Why Closing Exists

Two kinds of accounts live in your books, and they behave differently at year-end:

- **Permanent accounts** — Assets, Liabilities, Equity. These carry their balances *forever*. The bank balance on December 31 is the bank balance on January 1. They never reset; they're the running state of the business.
- **Temporary accounts** — Income and Expenses. These measure *one year's* activity, and at year-end they get swept to zero so next year starts fresh. Think of them as the year's scoreboard: at the final buzzer, you record the score and reset the board to 0–0 for next season.

**Closing the books** is that reset: you zero out every Income and Expense account and roll the net result — your $21,000 profit — into a permanent Equity account called **retained earnings**. "Retained earnings" is just bookkeeper-speak for *profit the business has kept over the years*.

#### T18 — The Closing Entry

**You say:**
> "Close out the year. Sweep 2026's profit into retained earnings."

**What happens:** The agent reads the year-end balance of every Income and Expense account and writes **one balanced transaction** that empties them all: it pushes each Income account back to zero, pushes each Expense account back to zero, and the difference between them — the $21,000 you actually made — lands in retained earnings. And it uses the same elision trick you met on day one: it names the income and expense amounts it's clearing, and lets the **retained earnings leg balance itself** to whatever's left over. What's left over is, by definition, the profit.

*(Under the hood — shown at the totals level for readability; the real call has one offsetting leg per Income and Expense account, each read from the year's balance: `ledger_record("2026-12-31", "Close FY2026 to retained earnings", [ +8,200,000 across the Income accounts, −6,100,000 across the Expense accounts, {Equity:RetainedEarnings (elided → −2,100,000)} ])`. The elided retained-earnings leg absorbs the **$21,000** net profit (raw `−2,100,000`). The whole thing still sums to zero, exactly like every other entry in this guide.)*

**You get back:**
> "Year closed. I zeroed out all your income and expense accounts and moved the **$21,000** profit into retained earnings. Your books are ready for 2027 — and your equity finally reflects the year you had."

**Say it your way:**
> • "Do the year-end close — roll this year's profit into equity."
> • "Sweep the income and expense accounts into retained earnings for 2026."
> • "Close the books on the year and bank the profit to my stake."

#### The After-Picture

Ask for the reports now, and watch what changed:

- **Income and Expenses** are back to **$0** — the temporary accounts reset, scoreboard cleared, ready for next year.
- **`Equity:RetainedEarnings`** now holds **$21,000** — the year's profit, made permanent.
- **Your equity on the balance sheet** now reads OwnerCapital $10,000 + RetainedEarnings $21,000 = **$31,000** — and *that's* your true stake in the shop: the money you put in, plus the profit you earned and kept. No more mysterious gap. The balance sheet finally tells the whole truth about what Greased Lightning Auto is worth to you.

> **Temporary accounts reset into permanent ones.** Closing the books is the one moment a year where Income and Expenses (the temporary scoreboard) empty themselves into Equity (the permanent record). After the close, your balance sheet's equity reflects every dollar of profit you've ever kept. That's the whole job of the closing entry — and it's still just one `ledger_record`, balanced to zero, with the profit elided onto the retained-earnings leg.

#### One Honest Caveat

Here's something worth knowing so you're never confused: **closing the books is a practice, not a requirement of this ledger.** Because every report here is *date-filtered*, you can ask for any year's profit-and-loss anytime — `period:"2026"`, `period:"2027"`, whatever — and get a clean answer, closed or not. The income and expense numbers for a given year are always recoverable from the dates.

So why close at all? For exactly one reason: **so the balance sheet's equity reflects your accumulated profit.** Without the closing entry, your stake forever reads as just the cash you contributed, and the year's earnings hang in limbo in the temporary accounts — the very gap that puzzled us back in June. There's no fiscal-period lock, no penalty, no door that slams shut on December 31. Closing is the standard, honest practice that makes your net worth read true. You do it once a year, in one sentence, and the books — like the shop — are square.

**Where we stand — the whole year, in one breath.** You opened a shop from nothing with a handshake and three sentences. You ran a month of real business — sales, invoices, bills, payroll, rent — entirely by saying what happened, and you fixed a mistake without ever erasing a thing. You reconciled against the bank to the penny, asked the books for every report a shop owner needs, and watched one stubborn invoice live its full life from June to July. Then you closed the year and turned a $21,000 profit into a permanent part of your stake. You never typed an account name. You never said "debit." You never balanced an entry by hand. You brought the story of Greased Lightning Auto; the agent brought the precision. That was the whole bet — and the coyote-free, grease-fire-free, *profitable* shop on December 31 is the bet made good.

---

## 9. Say It Your Way

Here's the secret the whole ledger is built around: **you never have to learn its words.** There are exactly eight things this service can do, and your assistant's job is to map whatever you actually said — "Moe paid cash for the transmission," "Burns still hasn't paid," "the rent check hasn't cleared yet" — onto one of them. You bring the plain-English sentence; the agent brings the account paths, the debit/credit signs, and the arithmetic.

So this section is a phrasebook. Same intent, a dozen ways to say it — all of them work, because they all land on the same eight tools underneath: `ledger_record`, `ledger_reverse`, `ledger_reconcile`, `ledger_balance`, `ledger_register`, `ledger_get`, `ledger_describe`, and `ledger_whoami`. Say it your way; the bookkeeping is on the house.

You'll notice there's no verb for "make an account," no verb for "send an invoice," no verb for "run a report." That's not an oversight — it's the whole design. An invoice is a `ledger_record` into an `Assets:Receivable:<customer>` account. A bill is a `ledger_record` into a `Liabilities:Payable:<vendor>` account. A report is a `ledger_balance` or `ledger_register` read. Accounts spring into existence the first time you post to them. You describe the *event*; the agent reaches for the right verb.

### The One Rule That Makes This Work

> **Loose in, corrective out.** You phrase the request like a person who runs an auto shop, not like an accountant. If something's off — the two sides of a transaction don't add up, you name an account whose root isn't one of the five real ones, you hand it a date that isn't a real calendar day, or you point at a transaction that doesn't exist — the service doesn't faceplant. It hands back a plain, typed error: `unbalanced`, `bad_root`, `validation`, `not_found`, or `already_reversed`. Your assistant reads that, fixes the entry, and retries. You usually never see the round trip.

That's the deal. You describe what happened in whatever words come naturally; the guardrails are downstream, and the agent absorbs them.

### Recording What Happened (`ledger_record`)

This is the heart of the ledger and the thing you'll do most — every sale, every bill, every paycheck, every deposit is one balanced transaction. You describe the event; the agent picks both accounts, assigns the signs, and makes the two sides sum to zero.

- "Moe paid cash for the $450 transmission job."
- "Put $450 in the till for that transmission, log it as labor."
- "We did a $450 transmission for Moe, cash."
- "Bill Ned Flanders $309 for the brake job — $200 labor, $100 in parts, $9 sales tax."
- "Invoice Mr. Burns for the engine overhaul, $1,854 on account."
- "Lenny and Carl's pay this period was $2,000."
- "Deposit the $450 from the drawer into checking."

Every one of those is a single `ledger_record`. You never typed `Assets:Cash:Drawer` or `Income:Service:Labor`, never said which side was the debit, never converted `$450` into `45000` cents. You said what happened. (Behind the curtain, Moe's transmission becomes `ledger_record("2026-06-04","Moe — transmission, cash",[{Assets:Cash:Drawer, +45000},{Income:Service:Labor (elided → −45000)}])` — the labor leg elided to take the balancing `−45000` — but you'll never see that unless you go looking.)

> **Say the number you know; the other side balances itself.** This is the killer convenience, and it's called *elision*. When you say "deposit the $450," you only ever stated *one* amount — and that's all the agent has to supply. Exactly one leg of a transaction can leave its amount blank, and the ledger fills it with whatever makes the books balance. So "put $10,000 of my own money into the shop" needs only the $10,000; the owner's-capital side works itself out. You can only leave *one* side blank, though — name two unknowns and there's nothing to solve for, and the service says so (`validation`).

### Recording a Bill or an Invoice (Still Just `ledger_record`)

Worth repeating, because it's the thing people expect to be a special feature and isn't:

- **An invoice** ("bill Burns $1,854 on account") is a `ledger_record` that puts the amount into `Assets:Receivable:Burns` — money owed *to* you.
- **A bill** ("Vaclav sent us a $600 parts bill, we'll pay later") is a `ledger_record` that puts the amount into `Liabilities:Payable:Vaclav` — money *you* owe.
- **A card purchase** ("$120 of brake pads on the shop card") is a `ledger_record` against `Liabilities:CreditCard`.
- **Getting paid** ("Burns finally paid his $1,854") is another `ledger_record` that moves the money from the receivable into the bank, closing the receivable out.

You say "invoice," "bill," "charge it," "on account," "they paid" — the agent routes each to the right account. There is no invoice object to manage; there's the journal, and the journal remembers everything.

### Fixing a Mistake (`ledger_reverse`)

You **cannot edit** a transaction and you **cannot delete** one — the journal is immutable, an honest append-only trail. So the way you fix a mistake is to *reverse* it (which posts a linked, sign-flipped mirror that cancels it out) and then re-record it correctly. You say "fix it"; the agent does the reverse-and-re-record two-step.

- "I fat-fingered the utilities bill — that should've been $150, not $1,500. Fix it."
- "Undo that last entry, it's wrong."
- "Back out the utilities transaction and let's redo it."
- "Scrap that one and re-enter it correctly."

The agent runs `ledger_reverse` on the wrong transaction — which leaves the original sitting in the books, posts its mirror (the same legs with their signs flipped, the mirror's description defaulting to *"Reversal of: …"* and its legs reset to `pending`), and links the two — then re-records the right numbers. All three entries stay: the mistake, the reversal, the correction. Nobody can quietly rewrite history.

> **Reverse the original once.** A transaction can only be reversed one time. Try to reverse something that's already been reversed and the service says `already_reversed` — if you need to undo *that*, you reverse the *mirror* instead. Your assistant knows this dance; you just say "fix it."

### Reconciling Against a Statement (`ledger_reconcile`)

This is the one move that changes an existing row — and even then it only ever touches a posting's **status**, never an amount, an account, or a date. Every posting starts life `pending`; when it shows up on your bank or card statement you mark it `cleared`; once the whole statement ties out you lock it `reconciled`. You say it in statement-speak; the agent flips the status.

- "These all cleared on the June statement — mark them cleared."
- "The rent check hasn't hit the bank yet, leave it."
- "Everything ties out — lock June in as reconciled."
- "That deposit shows up on the statement now."

Transitions are free in any direction (you can walk something back from `cleared` to `pending` if you need to). But it's **all-or-nothing**: hand it a posting that doesn't exist and the whole batch fails (`not_found`) rather than half-applying. Your assistant first pulls the relevant postings (a `ledger_register` over the bank account), grabs their ids, and then reconciles them — you never touch an id.

### Asking "What's the Balance?" (`ledger_balance`)

This single read is your trial balance, your balance sheet, your net worth, and your per-customer A/R, all at once. With nothing specified, it hands back the *whole* live chart of accounts. Narrow it by naming an account (or any piece of an account path), a time period, a depth to roll up to, or a reconciliation status.

- "What's in the checking account?"
- "How much does Burns owe me?"
- "Who owes me money right now?" → the agent queries `Assets:Receivable`
- "What are all my assets worth?"
- "What did we make in June?" → `Income` for the period
- "What did we spend in June?" → `Expenses` for the period
- "What's the shop worth overall?" → the roots rolled up
- "What's actually cleared the bank?" → the bank account, status `cleared`

Notice "who owes me money" and "what's in checking" are the *same verb* — just a different slice. You think in questions; the agent picks the filter. And the account match is a plain **substring**: "Receivable" finds every customer's receivable at once, so per-customer A/R falls out for free.

### Asking "What Happened, In Order?" (`ledger_register`)

Where `balance` gives you a *total*, `register` gives you the *story* — every matching posting in date order with a running total. This is a customer statement, an account history, a search, a "list everything that touched this account."

- "Show me Burns's account history." → his receivable, charge then payment, running to $0
- "Walk me through everything that hit the checking account."
- "List the transactions against the credit card."
- "Give me a statement for Flanders."
- "Show me every parts purchase this year."

A "customer statement" isn't a feature — it's a `ledger_register` over that customer's receivable. You say "statement"; the agent reads the register.

### Looking One Transaction Up (`ledger_get`)

When you want to see a single transaction in full — all its legs, each leg's status, and whether it's been reversed — that's `ledger_get`. You'll mostly hit this right before a correction ("let me look at that utilities entry") so you can see what you're about to reverse.

- "Pull up that utilities transaction."
- "Show me the full entry for Burns's invoice."
- "Let me see both sides of that one before I fix it."

### Finding Your Footing (`ledger_describe`)

This is the first call your assistant makes, before it does anything else — it's how the agent learns the five account types, the money unit, the reconciliation states, the live list of accounts you've actually used, and the recipes for building a balance sheet or P&L. You'll rarely ask for it by name, but it's why a brand-new ledger already knows what an "asset" is.

- "What can this thing track?"
- "What kinds of accounts do I have?"
- "How do the books work here?"

### The Handshake (`ledger_whoami`)

The quick "is this thing on?" check. It takes no input and just reports who the platform sees you as — your owner email and client id. It changes nothing. You reach for it once, right after you connect, to prove the whole chain lit up.

- "Am I connected to my ledger?"
- "Who does it think I am?"
- "Is the bookkeeping service actually online?"

### The Cheat Sheet

| What you want | Say something like… | Verb underneath |
|---|---|---|
| Record a sale, bill, invoice, payment, or pay run | "Moe paid $450 cash" · "bill Burns $1,854 on account" · "Vaclav's $600 parts bill" · "pay Lenny & Carl $2,000" | `ledger_record` |
| Fix a mistake | "that was wrong, fix it" · "undo that entry" · "back it out and redo it" | `ledger_reverse` (then `ledger_record`) |
| Mark items against a statement | "these cleared" · "lock June in as reconciled" | `ledger_reconcile` |
| Ask a balance / who owes me / what's it worth | "what's in checking" · "who owes me" · "what did we make in June" | `ledger_balance` |
| See an account's history or a customer statement | "Burns's statement" · "everything that hit checking" | `ledger_register` |
| Look one transaction up in full | "pull up that utilities entry" | `ledger_get` |
| Learn how the books are set up | "what kinds of accounts do I have" | `ledger_describe` |
| Check you're connected | "who am I" · "is this online" | `ledger_whoami` |

You don't memorize this table — your assistant does. You just keep talking like a shop owner. Say it your way; the eight verbs will be waiting.

---

## 10. Cheatsheet, Field Reference, Gotchas & FAQ

This is the one section you can mostly ignore — you run this ledger by *describing what happened*, and your assistant handles the exact accounts, signs, cents, and ids for you. But when you want to know precisely how the books behave — the kind of thing you'd double-check before quoting a number to the bank — this is the page to trust. It's the precise reference; accuracy here beats cleverness.

A reminder before the tables: **you never type any of this.** The account paths, signs, and tool names below are the vocabulary *the agent* speaks on your behalf. You still just say "Moe paid $450 cash."

### The Eight Tools (Real Signatures)

There is **one write entity — the balanced transaction** — and everything else is a read. The surface is a function of *verbs*, not entities, and **this set of eight never grows**.

| Tool | Signature | What it does |
|---|---|---|
| `ledger_record` | `(date, description, postings[], status?)` | Post one balanced double-entry transaction (2+ postings summing to zero). Returns the full transaction with the resolved residual and assigned ids. |
| `ledger_reverse` | `(id, date?, memo?)` | Post the sign-flipped mirror of a transaction, linked both ways. The correction primitive. Returns the mirror. |
| `ledger_reconcile` | `(posting_ids[], status)` | Transition the reconciliation status of one or more postings — the only mutation of existing rows. Returns the affected transactions. |
| `ledger_balance` | `(query?, period?, depth?, status?)` | The `bal` report and live chart of accounts. Returns `{lines:[{account, amount_cents}], total, unit}`. |
| `ledger_register` | `(query?, period?, status?)` | The `reg` report: matched postings in chronological order with a running total. |
| `ledger_get` | `(id)` | Fetch one transaction in full (all postings, per-posting status, order, reversal links). |
| `ledger_describe` | `()` | Discovery — the five roots, the unit, the reconciliation states, the live account tree, and report recipes. The first call an agent makes. |
| `ledger_whoami` | `()` | The authenticated caller's identity (owner email + client id). The end-to-end auth proof. |

> There is no `create_account`, no `ledger_report`, no `ledger_delete`, no `ledger_update`, and no invoice / bill / customer / vendor entity. Accounts are emergent (they appear on first posting); reports come from *recipes* over `balance` and `register`, not from tools.

**`ledger_record` postings.** Each posting is `{account, amount_cents, status?}`. There must be **2 or more**. `amount_cents` is signed minor units (debit `+`, credit `−`) and the signed amounts must **sum to exactly zero**. The transaction-level `status?` and each posting-level `status?` is one of `pending` / `cleared` / `reconciled`, defaulting to `pending`.

### The Five Account Roots (the Chart of Accounts)

Accounts are emergent colon-paths (`Assets:Bank:Checking`) — they spring into existence the first time you post to them; **no account is ever "created" as a step.** The only guardrail is that the **root** (the part before the first colon) must be one of these five. Sub-paths below the root are free-form. `Revenue` is an accepted alias of `Income`; root alias and case are folded so the tree can never fork, while sub-path case is preserved as you wrote it.

| Root | Normal balance | Feeds statement | Stored sign of its balance | Plain English |
|---|---|---|---|---|
| `Assets` | debit | balance sheet | **positive** | what you own |
| `Liabilities` | credit | balance sheet | **negative** | what you owe |
| `Equity` | credit | balance sheet | **negative** | your stake |
| `Income` (alias `Revenue`) | credit | income statement (P&L) | **negative** | what you earn |
| `Expenses` | debit | income statement (P&L) | **positive** | what you spend |

A `bad_root` error means an account named a root that isn't one of these five.

### The Reconciliation States (the Posting Lifecycle)

A status lives on each **posting** and defaults to `pending`. Transitions are free in any direction, including backward.

| Status | Meaning |
|---|---|
| `pending` | Recorded but not yet confirmed against an external source (the default). |
| `cleared` | Confirmed to have cleared the account — e.g. seen on the bank/card statement. |
| `reconciled` | Matched against an official statement balance and locked in. |

The lifecycle you'll walk for the bank: `pending → cleared → reconciled`.

### The Sign / Zero Convention (debit `+`, credit `−`, sum to zero)

- Money is **integer USD cents**, single currency. `$450.00` = `45000`; `$1,854.00` = `185400`. The unit string the tools return is `"USD cents"`.
- **Debit `+`, credit `−`.** Amounts are stored raw and signed, with no normalization.
- A single transaction's postings sum to **0**. A balance over **every** account also sums to **0** — that whole-ledger `total: 0` is a free correctness check the tools hand you on every `ledger_balance`.
- Reads return raw signed sums (ledger-cli convention): Assets and Expenses come back positive; Liabilities, Equity, and Income come back negative. The assistant flips the signs to show you numbers human-side-up (revenue and expenses read as positive dollars), using each root's published normal balance.

### The Elision Rule (say the number you know)

Exactly **one** posting in a `ledger_record` may omit its `amount_cents`; that leg receives the **balancing residual** — the negation of everything else, so the transaction nets to zero. Omitting the amount on *two or more* postings is a `validation` error (there's nothing to solve for). This is why "deposit the $450" or "put $10,000 of capital in" needs only the single number you actually know.

### Immutability & Reverse (no edit, no delete)

The journal **never changes**. There is no edit and no delete for journal facts. A mistake is fixed with `ledger_reverse`, which:

- posts a new transaction whose legs are the **sign-flipped mirror** of the original (whole transaction only — never a partial leg),
- links the two both ways (`reverses_id` on the mirror, `reversed_by_id` on the original),
- resets the mirror's legs to `pending` (a reversal hasn't cleared anything),
- and defaults the mirror's description to `"Reversal of: <original>"` (override with `memo?`) and its date to the original's (override with `date?`).

The original, the reversal, and the correction all stay in the journal — an honest, append-only trail. A transaction can be reversed only once; reversing an already-reversed one returns `already_reversed` (reverse its mirror instead). The lone exception that mutates an existing row is `ledger_reconcile`, and that touches **status only** — never an amount, account, or date.

### Filters: Period, Query, Depth, Status

These narrow the `ledger_balance` and `ledger_register` reads.

**`period`** — either a **bucket string** or an inclusive **range object**:

| Form | Example | Means |
|---|---|---|
| Year bucket | `"2026"` | all of 2026 |
| Month bucket | `"2026-06"` | all of June 2026 |
| Day bucket | `"2026-06-01"` | that single day |
| Range | `{"from":"2026-06-01","to":"2026-06-30"}` | inclusive `from`…`to` |

Omit `period` for "all time" / "now."

**`query`** — a **case-insensitive substring** matched against the full account path. `"Receivable"` matches `Assets:Receivable:Burns` *and* `Assets:Receivable:Flanders`; `"Bank"` matches every bank sub-account. Omit it to match every account. It is a literal substring, not fuzzy search — but your assistant rephrases for you, so you never feel that.

**`depth`** — roll accounts up to *N* colon-levels. `depth:1` collapses everything to the five roots (the all-roots roll-up the net-worth recipe builds on); omit it or pass `0` for full leaf accounts as posted.

**`status`** — restrict to postings in one reconciliation state. `status:"cleared"` over a bank account is exactly the cleared-vs-ledger bank-reconciliation view.

### The Recipes (reports without report tools)

Every statement is a recipe over `balance` / `register` — there is no report tool. These are the ones the ledger publishes via `ledger_describe`:

| Report | How |
|---|---|
| **Balance sheet** | `ledger_balance(query:"Assets")`, `(query:"Liabilities")`, `(query:"Equity")` — omit period for "now," or `{to:DATE}` for a point in time. |
| **Income statement (P&L)** | `ledger_balance(query:"Income", period:P)` and `(query:"Expenses", period:P)`; **net income = −(Income + Expenses)** (Income is stored negative). |
| **Net worth** | `ledger_balance(depth:1)`, then sum Assets + Liabilities + Equity (raw signed, so just add them). |
| **A/R — who owes me** | `ledger_balance(query:"Assets:Receivable")` — outstanding balance per customer sub-account. |
| **Customer statement** | `ledger_register(query:"Assets:Receivable:<customer>")` — chronological charges, payments, and running A/R balance. |
| **Bank reconciliation** | `ledger_balance(query:"Assets:Bank", status:"cleared")` vs. the same without a status filter; the difference is the uncleared items. |

A worked example from June: `ledger_balance(query:"Assets:Bank")` reads **$14,109** (the ledger balance), while `(query:"Assets:Bank", status:"cleared")` reads **$15,009** (cleared) — the **$900** difference between the two is the one outstanding rent check that hadn't hit the bank yet.

### The Error Vocabulary (what the agent self-corrects on)

These are typed; your assistant catches each and fixes the entry before you'd ever notice.

| Error | Means |
|---|---|
| `unbalanced` | the postings don't sum to zero |
| `bad_root` | an account's root isn't one of the five known types |
| `validation` | too few postings, more than one elided amount, a malformed account path, a bad date, or an unknown status |
| `not_found` | no transaction or posting with that id |
| `already_reversed` | the target transaction already has a reversal mirror |

### FAQ & Gotchas

**Do I need account codes or a chart of accounts to set up first?**
No. There are no account numbers and nothing to set up. Accounts are emergent: `Assets:Bank:Checking` exists the moment you first post to it. You describe what happened in plain English ("Moe paid cash for the transmission"); the agent picks the account paths. The only rule is that an account's *root* has to be one of the five types — and the agent handles that for you.

**Can I edit or delete a transaction I already recorded?**
No — the journal is immutable, no edits and no deletes. To fix a mistake you *reverse* it (which posts a linked, sign-flipped mirror that cancels it out) and then re-record it correctly, exactly like the utilities fat-finger in the story. All three entries — the mistake, the reversal, the fix — stay on the books. The result looks like a correction to you; underneath it's reverse-then-re-record. (The one thing you *can* change on an existing row is a posting's reconciliation status, via `ledger_reconcile` — and that only.)

**Why is income negative under the hood?**
Because the ledger stores raw signed amounts in the ledger-cli convention: debits are `+` and credits are `−`, and revenue is credit-normal, so `Income` accounts sum to a negative number internally. It's a sign convention, not a loss. Your assistant flips it so revenue reads as positive dollars when it talks back to you. The same is true of `Liabilities` and `Equity` (stored negative); `Assets` and `Expenses` are stored positive. The handy consequence: net income is `−(Income + Expenses)`, and a balance over *every* account sums to exactly zero — a free correctness check.

**What does "closing the books" mean?**
At year-end you sweep every `Income` and `Expenses` account to `$0` and roll the year's net profit into `Equity:RetainedEarnings`, with one balanced `ledger_record` the agent computes from the year's totals. In the story, FY2026's **$21,000** profit moves into retained earnings (the elided leg absorbs it), and the temporary accounts (Income/Expenses) reset to zero so the balance sheet's equity finally reflects accumulated profit. One honest caveat: because this ledger's reports are *date-filtered*, your P&L for any year is always available via `period:"YYYY"` regardless — so there's no fiscal-period lock and no "close" tool. You close mainly so the **balance sheet's equity reflects retained earnings**; it's a standard practice, not a feature.

**Is there a screen, an app, or a forms-based dashboard?**
No — and that's the point. There are no buttons, no forms, no screens, no debit/credit columns to fill in. The entire ledger is the eight things the agent can do on your behalf, driven by you describing what happened in plain language. This appendix exists for precision, not because you'll ever type any of it.
