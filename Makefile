prepare:
	python3 magnet-linux/prepare.py

test:
	python3 -m unittest discover -s magnet-linux/tests -p 'test_*.py'

.PHONY: prepare test
