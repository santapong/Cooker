// Shared form-state types for the New App wizard steps.

/** Recipe id — the pre-baked stage template picked (or auto-detected) for the app. */
export type RecipeId = 'go' | 'node-static' | 'worker';

export interface EnvCard {
  id: 'dev' | 'stg' | 'prod';
  title: string;
  sub: string;
  cluster: string;
  replicas: number;
  auto: boolean;
  selected: boolean;
}
