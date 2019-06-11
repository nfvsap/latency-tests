# -*- coding: utf-8 -*-
import flask
import os
import sys
import ast
import json
import argparse

app = flask.Flask(__name__)
filename = ''

@app.route('/latency_metrics')
def get_latency_percentiles():
    """ Retrieves the last saved latency hdr histogram percentiles
        and the average latency
        Args:
            -
        Returns:
            dict: A JSON object containing the metrics
    """
    status = 200
    return flask.Response(get_stats_json(),
                          status=status,
                          mimetype='application/json')

def get_stats_json():
    try:
        f = open(filename, 'r')
        line = f.readline()
        percentiles_dict = {}
        while line:
            percentile = line.split(' ')[0]
            latency = float(line.split(' ')[1])

            percentiles_dict[percentile] = latency
            line = f.readline()
        js = json.dumps(percentiles_dict, indent=2)
        f.close()
        return js

    except Exception as e:
        print("An exception occurred")
        print(str(e))



if __name__ == '__main__':
    parser = argparse.ArgumentParser(description='Give configuration options')
    parser.add_argument('--filename', metavar='filename', type=str,
                        help='the filename from which will retrieve the metrics')
    parser.add_argument('--port', metavar='port', type=int, default=5000,
                        help='Server port (default 5000)')
    args = parser.parse_args()
    filename = args.filename
    app.run(host='0.0.0.0', port=args.port, debug=True)

