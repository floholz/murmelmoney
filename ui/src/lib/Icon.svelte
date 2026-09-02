<script lang="ts">
  /** Tiny inline icon set (24px grid, stroke-based). */
  let { name, size = 22 }: { name: 'overview' | 'transactions' | 'recurring' | 'loans' | 'labels' | 'rules' | 'connect'; size?: number } = $props()
  const paths: Record<string, string> = {
    // pie chart
    overview: 'M12 3a9 9 0 1 0 9 9h-9V3z M14 2.5A9 9 0 0 1 21.5 10H14V2.5z',
    // list / receipt
    transactions: 'M6 3h12v18l-3-2-3 2-3-2-3 2V3z M9 8h6 M9 12h6 M9 16h4',
    // circular arrows
    recurring: 'M20 12a8 8 0 1 1-2.34-5.66 M20.5 3v4.5H16',
    // bank / columns
    loans: 'M3 9.5l9-5.5 9 5.5 M5 9.5V18 M9.7 9.5V18 M14.3 9.5V18 M19 9.5V18 M3 20h18',
    // tag
    labels: 'M3 12V4h8l9 9-8 8-9-9z M7.5 8.5h.01',
    // plug
    connect: 'M9 2v5 M15 2v5 M6 7h12v3a6 6 0 0 1-12 0V7z M12 16v6',
    // percent
    rules: 'M19 5L5 19 M7.5 9a1.5 1.5 0 1 0 0-3 1.5 1.5 0 0 0 0 3z M16.5 18a1.5 1.5 0 1 0 0-3 1.5 1.5 0 0 0 0 3z',
  }
</script>

<svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"
  stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d={paths[name]} /></svg>
