// Every string the app itself shows or speaks. Labels, names, times, counts
// and statuses only: no hints, captions, instructions or empty-state
// sentences (PL-01). test/app/test_no_explanatory_strings_test.dart checks
// [S.all] and that no screen carries a string literal of its own.
abstract final class S {
  static const plebian = 'Plebian';
  static const today = 'Today';
  static const journal = 'Journal';
  static const open = 'Open';
  static const back = 'Back';
  static const week = 'This week';

  static const omi = 'Omi';
  static const live = 'live';
  static const syncing = 'syncing';
  static const outOfRange = 'out of range';
  static const unreachable = 'unreachable';
  static const queued = 'queued';
  static const refused = 'refused';

  static const days = 'Days';
  static const taps = 'Taps';
  static const search = 'Search';
  static const heard = 'heard';
  static const tap = 'tap';
  static const tapsCount = 'taps';
  static const openCount = 'open';
  static const messages = 'messages';

  static const entry = 'Entry';
  static const withPlebian = 'With Plebian';
  static const heardTitle = 'Heard';
  static const you = 'you';

  static const message = 'Message';
  static const send = 'Send';
  static const removeQuote = 'Remove quote';
  static const quote = 'Quote';

  static const draft = 'Draft';
  static const question = 'Question';
  static const asDrafted = 'as drafted';
  static const changed = 'changed';
  static const turnedDown = 'turned down';
  static const browser = 'Browser';
  static const questions = 'Questions';
  static const drafts = 'Drafts';
  static const orders = 'Kept orders';

  static const tapped = 'tapped';
  static const tappedOnOmi = 'tapped on the Omi';
  static const pressedInApp = 'pressed in the app';
  static const note = 'Note';
  static const save = 'Save';
  static const play = 'Play';
  static const pause = 'Pause';

  static const state = 'State';
  static const battery = 'Battery';
  static const firmware = 'Firmware';
  static const waiting = 'Waiting';

  static const breakage = 'Breakage';
  static const security = 'Security';
  static const ideas = 'Ideas';
  static const standing = 'Standing';
  static const replay = 'Replay';
  static const approve = 'Approve';
  static const grant = 'Grant';
  static const deploy = 'Deploy';
  static const health = 'Health';
  static const spend = 'Spend';
  static const tokensIn = 'in';
  static const tokensOut = 'out';
  static const cached = 'cached';
  static const setup = 'Setup';
  static const required = 'Required';

  static const server = 'Server';
  static const pair = 'Pair';
  static const model = 'Model';
  static const mail = 'Mail';
  static const calendar = 'Calendar';
  static const texts = 'Texts';
  static const storage = 'Storage';
  static const backups = 'Backups';
  static const updates = 'Updates';
  static const check = 'Check';
  static const connect = 'Connect';
  static const code = 'Code';
  static const scan = 'Scan';
  static const commit = 'Commit';
  static const version = 'Version';
  static const endpoint = 'Endpoint';
  static const key = 'Key';
  static const imap = 'IMAP';
  static const smtp = 'SMTP';
  static const user = 'User';
  static const password = 'Password';
  static const url = 'URL';
  static const bucket = 'Bucket';
  static const secret = 'Secret';
  static const region = 'Region';
  static const secondDisk = 'Second disk';
  static const offsite = 'Offsite';
  static const githubToken = 'GitHub token';
  static const mechanic = 'Mechanic';
  static const backupKey = 'Backup key';
  static const emoji = 'Emoji';
  static const roleMain = 'Main';
  static const roleSub = 'Sub';
  static const roleStandIn = 'Stand-in';
  static const roleFallback = 'Fallback';

  static const take = 'Take';
  static const give = 'Give';
  static const keys = 'Keys';

  static const weekdays = ['Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday', 'Sunday'];
  static const months = ['JAN', 'FEB', 'MAR', 'APR', 'MAY', 'JUN', 'JUL', 'AUG', 'SEP', 'OCT', 'NOV', 'DEC'];
  static const am = 'am';
  static const pm = 'pm';
  static const dot = ' · ';

  static const all = <String>[
    plebian, today, journal, open, back, week, omi, live, syncing, outOfRange, unreachable, queued, refused,
    days, taps, search, heard, tap, tapsCount, openCount, messages, entry, withPlebian, heardTitle, you,
    message, send, removeQuote, quote, draft, question, asDrafted, changed, turnedDown, browser, questions,
    drafts, orders, tapped, tappedOnOmi, pressedInApp, note, save, play, pause, state, battery, firmware,
    waiting, breakage, security, ideas, standing, replay, approve, grant, deploy, health, spend, tokensIn,
    tokensOut, cached, setup, required, server, pair, model, mail, calendar, texts, storage, backups,
    updates, check, connect, code, scan, commit, version, endpoint, key, imap, smtp, user, password, url,
    bucket, secret, region, secondDisk, offsite, githubToken, mechanic, backupKey, emoji, roleMain, roleSub,
    roleStandIn, roleFallback, take, give, keys, ...weekdays, ...months, am, pm, dot,
  ];
}
