#!/usr/bin/env python3
"""Generate known tones and verify them in QEMU's recorded PCM output."""
import array
import io
import math
import pathlib
import sys
import wave


def tone(frequency, seconds=2, rate=48000):
    samples = array.array('h', (
        round(4000 * math.sin(2 * math.pi * frequency * n / rate))
        for n in range(rate * seconds)
    ))
    if sys.byteorder != 'little':
        samples.byteswap()
    output = io.BytesIO()
    with wave.open(output, 'wb') as wav:
        wav.setparams((1, 2, rate, 0, 'NONE', 'not compressed'))
        wav.writeframes(samples.tobytes())
    return output.getvalue()


def check(path, frequencies):
    with wave.open(str(path), 'rb') as wav:
        assert wav.getsampwidth() == 2, 'Expected signed 16-bit PCM from QEMU'
        channels, rate = wav.getnchannels(), wav.getframerate()
        samples = array.array('h', wav.readframes(wav.getnframes()))
    if sys.byteorder != 'little':
        samples.byteswap()
    # Use the first channel at a lower sampling rate; all test tones are below
    # its Nyquist frequency. Evaluate half-second windows to tolerate silence,
    # device startup delays and unrelated sounds between the test segments.
    samples = samples[::channels * 8]
    rate /= 8
    size = int(rate / 2)
    found = set()
    for start in range(0, len(samples) - size, size):
        chunk = samples[start:start + size]
        rms = math.sqrt(sum(x*x for x in chunk) / size)
        if rms < 100:
            continue
        for frequency in frequencies - found:
            step = 2 * math.pi * frequency / rate
            re = sum(x * math.cos(step*n) for n, x in enumerate(chunk))
            im = sum(x * math.sin(step*n) for n, x in enumerate(chunk))
            amplitude = 2 * math.hypot(re, im) / size
            if amplitude > 150 and amplitude / rms > 0.9:
                found.add(frequency)
        if found == frequencies:
            break
    assert found == frequencies, f'Missing tones: {sorted(frequencies - found)}; found {sorted(found)}'
    print(f'PASS: QEMU output contains tones at {sorted(found)} Hz')


if __name__ == '__main__':
    check(pathlib.Path(sys.argv[1]), {int(value) for value in sys.argv[2:]})
