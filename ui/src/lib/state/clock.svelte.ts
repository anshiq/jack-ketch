class Clock {
  now = $state(Date.now());
  private refs = 0;
  private rate = 1000;
  private timer: ReturnType<typeof setInterval> | null = null;

  private start() {
    if (this.timer) clearInterval(this.timer);
    this.now = Date.now();
    this.timer = setInterval(() => (this.now = Date.now()), this.rate);
  }

  retain(ms = 1000): () => void {
    this.refs++;
    const next = Math.max(250, ms);
    if (!this.timer || next < this.rate) {
      this.rate = next;
      this.start();
    }
    return () => {
      this.refs--;
      if (this.refs <= 0) {
        if (this.timer) clearInterval(this.timer);
        this.timer = null;
        this.rate = 1000;
        this.refs = 0;
      }
    };
  }
}

export const clock = new Clock();
