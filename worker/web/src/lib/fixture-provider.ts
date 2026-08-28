export async function fixtureRequest<T>(_path: string, _init: RequestInit = {}): Promise<T> {
  throw new Error("Fixture data is unavailable in this dashboard build.");
}
