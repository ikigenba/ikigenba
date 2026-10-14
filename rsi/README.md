# rsi

Recursive self-improvement, done by hand. The skills under `.agents/skills/` run the delivery process, and the process cannot measure or change itself. So every week or two a person and an agent measure one delivery, guess at what would make it faster or cheaper without making it worse, change the skills, and write it down here.

One file per cycle, named by date. Each records the sources measured, the baseline numbers, every skill change with the commit and the measurement it aims at, what was left alone, and what the next cycle should expect to see if the changes worked.

The next cycle reads the previous file first, measures the same things, and judges each change by the numbers before adding more. A change that made things worse is reverted and the reversion recorded.

Not a sub-project: nothing here is built or tested, and no skill reads it.
