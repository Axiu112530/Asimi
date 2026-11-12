declare module '*/runtime.js' {
  export const Call: { ByName(name: string, ...args: unknown[]): Promise<unknown> };
  export const Events: { On(name: string, callback: (event: { data: unknown }) => void): (() => void) };
}
declare module '*/acceptance.js';
declare module '*.css';
