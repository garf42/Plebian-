# First run with the real Omi

The build tests the Omi through a simulator. These checks prove the real device on the owner's phone. Each line is pass/fail; a failure goes to `build/gaps.md` with what was seen.

1. The app finds the Omi and pairs; the Omi screen (tap the Omi chip in the header) shows its firmware revision and battery.
2. Talk for a minute with someone nearby. Today's ribbon fills while you talk; within a minute of the conversation ending its lines appear on the day page under two different speaker labels.
3. Talk with them again. Each voice now heard twice with no name (yours too) is a question in Open; answer each in your own words ("that's me", "that was …"). The next conversation carries the names.
4. Double-tap the Omi near the phone: one short buzz, and a mark on the ribbon at that minute.
5. Turn the phone's Bluetooth off, talk for a minute, double-tap (no mark; a long buzz only with Plebian's firmware), turn Bluetooth back on. The minute syncs and lands at the time it was said.
6. Walk out of Wi-Fi range with Tailscale on: send the agent a message; it answers.
7. Hold the Omi's button three seconds: it powers off, and the Omi chip says so.
