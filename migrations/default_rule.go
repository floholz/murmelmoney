package migrations

// DefaultRule is the JavaScript body of the tax rule that ships with a fresh
// install. It receives the yearly aggregate `d` (see ui/src/lib/tax.ts) and
// must return an array of {label, value, hint?} lines. Edit it in the UI.
// Available: d.income, d.expenses, d.net, d.area.<business|rental|private>,
// d.category[name], d.tag[name] (each {income, expenses, net}) and d.transactions[].
const DefaultRule = `// Rough Austrian estimate for a freelancer who also has rental income.
// Everything below is an assumption — edit freely. Numbers are 2025 values.
const brackets = [ // Einkommensteuer tariff [upTo, rate]
  [13308, 0.00], [21617, 0.20], [35836, 0.30], [69166, 0.40],
  [103072, 0.48], [1000000, 0.50], [Infinity, 0.55],
];
const svRate  = 0.2683;               // SVS: PV 18.5% + KV 6.8% + Selbständigenvorsorge 1.53%
const gfbRate = 0.15, gfbCap = 4950;  // Grundfreibetrag: 15% of first 33 000

const biz  = d.area.business.net;
const rent = d.area.rental.net;
const sv   = Math.max(0, biz * svRate);
const gfb  = Math.min(gfbCap, Math.max(0, biz) * gfbRate);
const taxable = Math.max(0, biz - sv - gfb) + Math.max(0, rent);

let tax = 0, prev = 0;
for (const [upTo, rate] of brackets) {
  if (taxable <= prev) break;
  tax += (Math.min(taxable, upTo) - prev) * rate;
  prev = upTo;
}

return [
  { label: 'Business profit',          value: biz },
  { label: 'Rental profit',            value: rent },
  { label: 'SVS contributions (est.)', value: -sv,  hint: (svRate * 100).toFixed(2) + '% of business profit' },
  { label: 'Gewinnfreibetrag',         value: -gfb, hint: 'Grundfreibetrag only' },
  { label: 'Taxable income',           value: taxable },
  { label: 'Einkommensteuer (est.)',   value: tax },
  { label: 'Effective tax rate',       value: (taxable ? tax / taxable * 100 : 0).toFixed(1) + ' %' },
  { label: 'Set aside (SVS + ESt)',    value: sv + tax, hint: 'what to keep on the side this year' },
];
`
