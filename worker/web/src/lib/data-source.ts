export function fixturesAllowed(source: string | undefined, development: boolean, mode: string) {
  return source === "fixtures" && (development || mode === "demo");
}

export const fixtureDataEnabled = __MIMIR_FIXTURES__;

export const demoMode = import.meta.env.MODE === "demo";
