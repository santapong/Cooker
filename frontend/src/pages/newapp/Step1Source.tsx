import type { Dispatch, SetStateAction } from 'react';
import { Card, Input, Label } from '../../components/ui/atoms';
import StepShell from './StepShell';

interface Step1SourceProps {
  name: string;
  setName: Dispatch<SetStateAction<string>>;
  repo: string;
  setRepo: Dispatch<SetStateAction<string>>;
  branch: string;
  setBranch: Dispatch<SetStateAction<string>>;
}

export default function Step1Source({
  name,
  setName,
  repo,
  setRepo,
  branch,
  setBranch,
}: Step1SourceProps) {
  return (
    <StepShell
      eyebrow="Step 1"
      title="What are you deploying?"
      body="Point Cooker at a GitHub repo. We'll detect the language and suggest a recipe in the next step."
    >
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 14 }}>
        <Card>
          <Label>App name</Label>
          <Input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="checkout-api"
          />
        </Card>
        <Card>
          <Label>GitHub repo (owner/name)</Label>
          <Input
            value={repo}
            onChange={(e) => setRepo(e.target.value)}
            placeholder="octocat/Hello-World"
          />
          <Label>Branch</Label>
          <Input
            value={branch}
            onChange={(e) => setBranch(e.target.value)}
            placeholder="main"
          />
        </Card>
      </div>
    </StepShell>
  );
}
