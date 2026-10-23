// Asking for a line of text (a collection's name): PromptDialog (shown by App, a Modal). `await askText({...})` is
// the text typed (trimmed), or null when canceled.

export interface PromptOptions {
  title: string;
  label: string; // the box's accessible name
  value?: string;
  placeholder?: string;
  action: string; // the button ("Create", "Save")
}

class Prompt {
  current = $state<PromptOptions | null>(null);
  private resolve: ((v: string | null) => void) | null = null;

  ask(o: PromptOptions): Promise<string | null> {
    this.resolve?.(null);
    this.current = o;
    return new Promise((r) => (this.resolve = r));
  }

  answer(v: string | null) {
    this.current = null;
    this.resolve?.(v === null ? null : v.trim() || null);
    this.resolve = null;
  }
}

export const prompt = new Prompt();
export const askText = (o: PromptOptions) => prompt.ask(o);
