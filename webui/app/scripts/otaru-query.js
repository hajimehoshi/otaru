"use strict";

class OtaruQuery {
  constructor(opts) {
    if (opts.endpointURL === undefined) { throw "endpointURL required!"; }
    if (opts.onData === undefined) { throw "onData required!"; }

    this.endpointURL = opts.endpointURL;
    this.onData = opts.onData;
    this.onError = opts.onError || this._defaultOnError;
    this.text = opts.text || false;
    this.requestInterval = opts.requestInterval || 3000;
    this.oneShot = opts.oneShot || false;

    this.shouldFetch = false; 
    this.state = 'inactive';
    this.timer = null;
  }

  start() {
    this.shouldFetch = true; 
    this._requestIfNeeded();
  }

  stop() {
    this.shouldFetch = false;
  }

  _waitAndRequestIfNeeded() {
    if (this.shouldFetch) {
      this.state = 'wait';
      this.timer = setTimeout(() => this._requestIfNeeded(), this.requestInterval);
    }
  }

  _requestIfNeeded() {
    if (this.timer) {
      clearTimeout(this.timer);
      this.timer = null;
    }
    if (this.shouldFetch) {
      let f = fetch(this.endpointURL).catch(this._onError.bind(this))
      if (this.text) {
        f = f.then((res) => {
          if (res === undefined) return;
          return res.text();
        });
      } else {
        f = f.then((res) => {
          if (res === undefined) return;
          return res.json();
        });
      }
      f.then(this._onResponse.bind(this));
      this.state = 'inflight';
    } else {
      this.state = 'inactive';
    }
  }

  _onResponse(data) {
    if (data !== undefined) {
      this.onData(data);
    }
    if (this.oneShot) {
      this.shouldFetch = false; 
    }
    this._waitAndRequestIfNeeded();
  }

  _onError(err) {
    this.onError(err);
    this._waitAndRequestIfNeeded();
  }

  _defaultOnError(err) {
    // do nothing
  }
}
